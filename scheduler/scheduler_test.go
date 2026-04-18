package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"kingshot-redeemer/config"
	"kingshot-redeemer/store"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// mockStore is a thread-safe in-memory store for testing.
type mockStore struct {
	mu       sync.Mutex
	redeemed map[string]bool
	saved    []store.Redemption
}

func newMockStore() *mockStore {
	return &mockStore{redeemed: make(map[string]bool)}
}

func (m *mockStore) IsRedeemed(playerID, code string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.redeemed[playerID+"|"+code], nil
}

func (m *mockStore) SaveRedemption(r store.Redemption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.redeemed[r.PlayerID+"|"+r.Code] = true
	m.saved = append(m.saved, r)
	return nil
}

func (m *mockStore) Close() error { return nil }

func healthServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "healthy",
			"checks": map[string]any{"database": "ok", "server": "ok"},
		})
	}))
}

func codesServer(codes []map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"giftCodes":    codes,
				"total":        len(codes),
				"activeCount":  len(codes),
				"expiredCount": 0,
			},
		})
	}))
}

func redeemServer(results []map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, res := range results {
			data, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
}

func playerFile(t *testing.T, ids []string) string {
	t.Helper()
	f, err := os.CreateTemp("", "players*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	for _, id := range ids {
		fmt.Fprintln(f, id)
	}
	f.Close()
	return f.Name()
}

func TestTick_redeemsAndSaves(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer([]map[string]any{
		{"accountId": "p1", "status": "success", "message": "OK", "playerInfo": map[string]any{"nickname": "Hero", "kingdom": 1}},
	})
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize: 		1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Errorf("saved: got %d, want 1", len(s.saved))
	}
	if s.saved[0].PlayerID != "p1" || s.saved[0].Code != "CODE1" {
		t.Errorf("saved: got %+v", s.saved[0])
	}
}

func TestTick_skipsAlreadyRedeemed(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer(nil) // should not be called
	defer redeemSrv.Close()

	s := newMockStore()
	s.redeemed["p1|CODE1"] = true

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize: 		1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 0 {
		t.Errorf("expected nothing saved, got %d", len(s.saved))
	}
}

func TestTick_expiredCodeSaved(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "KS0408", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer([]map[string]any{
		{"accountId": "p1", "status": "error", "message": "Gift code expired."},
	})
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize: 		1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Fatalf("expected expired code to be saved, got %d entries", len(s.saved))
	}
	if s.saved[0].PlayerID != "p1" || s.saved[0].Code != "KS0408" {
		t.Errorf("saved: got %+v", s.saved[0])
	}
}

func TestTick_expiredCodeNotRetried(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "KS0408", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer(nil) // must not be called
	defer redeemSrv.Close()

	s := newMockStore()
	s.redeemed["p1|KS0408"] = true // already saved from previous expired tick

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize: 		1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 0 {
		t.Errorf("expected no retry, got %d saves", len(s.saved))
	}
}

func TestTick_alreadyRedeemedSaved(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer([]map[string]any{
		{"accountId": "p1", "status": "error", "message": "Gift code already redeemed."},
	})
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Fatalf("expected already-redeemed to be saved, got %d entries", len(s.saved))
	}
	if s.saved[0].Status != store.StatusAlreadyRedeemed {
		t.Errorf("status: got %q, want %q", s.saved[0].Status, store.StatusAlreadyRedeemed)
	}
}

func TestTick_alreadyRedeemedNotRetried(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer(nil) // must not be called
	defer redeemSrv.Close()

	s := newMockStore()
	s.redeemed["p1|CODE1"] = true

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 0 {
		t.Errorf("expected no retry, got %d saves", len(s.saved))
	}
}

func TestTick_unknownErrorNotSaved(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer([]map[string]any{
		{"accountId": "p1", "status": "error", "message": "Internal server error."},
	})
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 0 {
		t.Errorf("unknown error should not be saved (allow retry), got %d saves", len(s.saved))
	}
}

func TestTick_expiredStatusSaved(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "KS0408", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := redeemServer([]map[string]any{
		{"accountId": "p1", "status": "error", "message": "Gift code expired."},
	})
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Fatalf("expected expired code to be saved, got %d entries", len(s.saved))
	}
	if s.saved[0].Status != store.StatusExpired {
		t.Errorf("status: got %q, want %q", s.saved[0].Status, store.StatusExpired)
	}
}

func TestTick_parallelBatches(t *testing.T) {
	const batchDelay = 80 * time.Millisecond
	const numPlayers = 9
	const batchSize = 3 // 3 batches total

	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	// Server parses accountIds from request body, returns a success result per ID.
	// Sleeps batchDelay to make sequential vs parallel timing distinguishable.
	slowRedeemSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccountIDs []string `json:"accountIds"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		time.Sleep(batchDelay)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, id := range body.AccountIDs {
			data, _ := json.Marshal(map[string]any{
				"accountId": id,
				"status":    "success",
				"message":   "OK",
				"playerInfo": map[string]any{"nickname": id, "kingdom": 1},
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	defer slowRedeemSrv.Close()

	players := make([]string, numPlayers)
	for i := range players {
		players[i] = fmt.Sprintf("p%d", i+1)
	}

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, players),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    slowRedeemSrv.URL,
		BatchSize:    batchSize,
		Workers:      3,
	}

	start := time.Now()
	tick(context.Background(), cfg, s)
	elapsed := time.Since(start)

	s.mu.Lock()
	savedCount := len(s.saved)
	s.mu.Unlock()

	if savedCount != numPlayers {
		t.Errorf("saved: got %d, want %d", savedCount, numPlayers)
	}

	// Sequential would take 3 * batchDelay. Parallel should be ~1 * batchDelay.
	// Allow generous 2x threshold to avoid flakiness.
	maxExpected := 2 * batchDelay
	if elapsed > maxExpected {
		t.Errorf("elapsed %v suggests sequential execution (want < %v for parallel)", elapsed, maxExpected)
	}
}

func TestFilterUnredeemed(t *testing.T) {
	s := newMockStore()
	s.redeemed["p1|CODE1"] = true

	remaining := filterUnredeemed(s, []string{"p1", "p2", "p3"}, "CODE1")
	if len(remaining) != 2 {
		t.Errorf("got %d, want 2: %v", len(remaining), remaining)
	}
	for _, id := range remaining {
		if id == "p1" {
			t.Error("p1 should be filtered out")
		}
	}
}
