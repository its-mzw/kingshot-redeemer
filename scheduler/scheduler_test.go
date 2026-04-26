package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"kingshot-redeemer/config"
	"kingshot-redeemer/redeemer"
	"kingshot-redeemer/store"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// captureLog redirects the global logger to a buffer for the duration of the test.
// Not t.Parallel()-safe — tests using this must run sequentially.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

// countLines counts lines in s that contain substr.
func countLines(s, substr string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// echoRedeemServer returns a server that reads accountIds from the request body
// and streams one SSE result per ID with the given status and message.
func echoRedeemServer(status, message string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccountIDs []string `json:"accountIds"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, id := range body.AccountIDs {
			result := map[string]any{"accountId": id, "status": status, "message": message}
			if status == "success" {
				result["playerInfo"] = map[string]any{"nickname": id, "kingdom": 1}
			}
			data, _ := json.Marshal(result)
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

func skippingFile(t *testing.T, codes []string) string {
	t.Helper()
	f, err := os.CreateTemp("", "skipping*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	for _, c := range codes {
		fmt.Fprintln(f, c)
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
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
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
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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
		SkippingFile: skippingFile(t, nil),
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

func TestLogging_cleanRun(t *testing.T) {
	buf := captureLog(t)

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"}})
	defer codesSrv.Close()
	redeemSrv := echoRedeemServer("success", "OK")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile: playerFile(t, []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:  healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		BatchSize: 3, Workers: 3,
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, `redeeming "CODE1" for 9 players (3 batches, 3 workers)`) != 1 {
		t.Errorf("missing start line; got:\n%s", out)
	}
	if countLines(out, "picking up 3 ids") != 3 {
		t.Errorf("want 3 pickup lines, got %d; output:\n%s", countLines(out, "picking up"), out)
	}
	if countLines(out, "workers busy") != 3 {
		t.Errorf("pickup lines missing 'workers busy'; got:\n%s", out)
	}
	if countLines(out, `"CODE1" done — 9 succeeded, 0 failed`) != 1 {
		t.Errorf("missing done line; got:\n%s", out)
	}
	if countLines(out, "player p") != 0 {
		t.Errorf("unexpected per-player log lines; got:\n%s", out)
	}
}

func TestLogging_expiredCode(t *testing.T) {
	buf := captureLog(t)

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"}})
	defer codesSrv.Close()
	redeemSrv := echoRedeemServer("error", "Gift code expired.")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile: playerFile(t, []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:  healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		BatchSize: 3, Workers: 3,
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, "picking up") != 3 {
		t.Errorf("want 3 pickup lines; got:\n%s", out)
	}
	if countLines(out, `"CODE1" done — 0 succeeded, 9 failed (9 expired)`) != 1 {
		t.Errorf("missing done line with expired count; got:\n%s", out)
	}
	if countLines(out, "player p") != 0 {
		t.Errorf("expired should not produce per-player logs; got:\n%s", out)
	}
}

func TestLogging_unknownError(t *testing.T) {
	buf := captureLog(t)

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"}})
	defer codesSrv.Close()
	redeemSrv := echoRedeemServer("error", "Internal server error.")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		BatchSize: 1, Workers: 1,
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, `player p1 "CODE1": Internal server error.`) != 1 {
		t.Errorf("want per-player unknown error line; got:\n%s", out)
	}
	if countLines(out, "1 unknown") != 1 {
		t.Errorf("want done line with '1 unknown'; got:\n%s", out)
	}
}

func TestLogging_mixedFailures(t *testing.T) {
	buf := captureLog(t)

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"}})
	defer codesSrv.Close()

	// Each request gets a different result based on call order.
	var mu sync.Mutex
	responses := []map[string]any{
		{"status": "error", "message": "Gift code expired."},
		{"status": "error", "message": "Gift code already redeemed."},
		{"status": "success", "message": "OK"},
	}
	idx := 0
	mixedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ AccountIDs []string `json:"accountIds"` }
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, id := range body.AccountIDs {
			mu.Lock()
			res := responses[idx%len(responses)]
			idx++
			mu.Unlock()
			entry := map[string]any{"accountId": id, "status": res["status"], "message": res["message"]}
			if res["status"] == "success" {
				entry["playerInfo"] = map[string]any{"nickname": id, "kingdom": 1}
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	defer mixedSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1", "p2", "p3"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: mixedSrv.URL,
		BatchSize: 1, Workers: 1, // sequential so response order is deterministic
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, `done — 1 succeeded, 2 failed (1 expired, 1 already_redeemed)`) != 1 {
		t.Errorf("want done line with breakdown; got:\n%s", out)
	}
	if countLines(out, "player p") != 0 {
		t.Errorf("expired/already_redeemed should not produce per-player logs; got:\n%s", out)
	}
}

func TestFailureSuffix(t *testing.T) {
	cases := []struct {
		expired, alreadyRedeemed, unknown int
		want                              string
	}{
		{0, 0, 0, ""},
		{3, 0, 0, " (3 expired)"},
		{0, 2, 0, " (2 already_redeemed)"},
		{0, 0, 1, " (1 unknown)"},
		{1, 2, 3, " (1 expired, 2 already_redeemed, 3 unknown)"},
		{0, 1, 1, " (1 already_redeemed, 1 unknown)"},
		{1, 0, 1, " (1 expired, 1 unknown)"},
	}
	for _, tc := range cases {
		got := failureSuffix(tc.expired, tc.alreadyRedeemed, tc.unknown)
		if got != tc.want {
			t.Errorf("failureSuffix(%d,%d,%d) = %q, want %q",
				tc.expired, tc.alreadyRedeemed, tc.unknown, got, tc.want)
		}
	}
}

func TestProcessSummary(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("success saves with player info", func(t *testing.T) {
		s := newMockStore()
		summary := &redeemer.Summary{
			Timestamp: ts,
			Results: []redeemer.Result{
				{AccountID: "p1", Status: "success", Message: "OK",
					PlayerInfo: &redeemer.PlayerInfo{Nickname: "Hero", Kingdom: 7}},
			},
		}
		succ, exp, ar, unk := processSummary(s, "CODE1", summary)
		if succ != 1 || exp != 0 || ar != 0 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 1/0/0/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 {
			t.Fatalf("want 1 saved, got %d", len(s.saved))
		}
		r := s.saved[0]
		if r.Status != store.StatusSuccess || r.Nickname != "Hero" || r.Kingdom != 7 {
			t.Errorf("saved: %+v", r)
		}
	})

	t.Run("expired saved and counted", func(t *testing.T) {
		s := newMockStore()
		summary := &redeemer.Summary{
			Timestamp: ts,
			Results: []redeemer.Result{
				{AccountID: "p1", Status: "error", Message: "Gift code expired."},
			},
		}
		succ, exp, ar, unk := processSummary(s, "CODE1", summary)
		if succ != 0 || exp != 1 || ar != 0 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/1/0/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 || s.saved[0].Status != store.StatusExpired {
			t.Errorf("want expired saved, got %+v", s.saved)
		}
	})

	t.Run("already redeemed saved and counted", func(t *testing.T) {
		s := newMockStore()
		summary := &redeemer.Summary{
			Timestamp: ts,
			Results: []redeemer.Result{
				{AccountID: "p1", Status: "error", Message: "Gift code already redeemed."},
			},
		}
		succ, exp, ar, unk := processSummary(s, "CODE1", summary)
		if succ != 0 || exp != 0 || ar != 1 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/0/1/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 || s.saved[0].Status != store.StatusAlreadyRedeemed {
			t.Errorf("want already_redeemed saved, got %+v", s.saved)
		}
	})

	t.Run("unknown error not saved", func(t *testing.T) {
		s := newMockStore()
		summary := &redeemer.Summary{
			Timestamp: ts,
			Results: []redeemer.Result{
				{AccountID: "p1", Status: "error", Message: "Internal server error."},
			},
		}
		succ, exp, ar, unk := processSummary(s, "CODE1", summary)
		if succ != 0 || exp != 0 || ar != 0 || unk != 1 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/0/0/1", succ, exp, ar, unk)
		}
		if len(s.saved) != 0 {
			t.Errorf("unknown error should not be saved, got %+v", s.saved)
		}
	})

	t.Run("mixed results", func(t *testing.T) {
		s := newMockStore()
		summary := &redeemer.Summary{
			Timestamp: ts,
			Results: []redeemer.Result{
				{AccountID: "p1", Status: "success", Message: "OK",
					PlayerInfo: &redeemer.PlayerInfo{Nickname: "A", Kingdom: 1}},
				{AccountID: "p2", Status: "error", Message: "Gift code expired."},
				{AccountID: "p3", Status: "error", Message: "Gift code already redeemed."},
				{AccountID: "p4", Status: "error", Message: "Unexpected."},
			},
		}
		succ, exp, ar, unk := processSummary(s, "CODE1", summary)
		if succ != 1 || exp != 1 || ar != 1 || unk != 1 {
			t.Errorf("counts: got %d/%d/%d/%d, want 1/1/1/1", succ, exp, ar, unk)
		}
		if len(s.saved) != 3 { // success + expired + already_redeemed; unknown not saved
			t.Errorf("want 3 saved, got %d: %+v", len(s.saved), s.saved)
		}
	})
}

func TestContains(t *testing.T) {
	list := []string{"A", "B", "C"}
	if !contains(list, "B") {
		t.Error("expected true for element in list")
	}
	if contains(list, "D") {
		t.Error("expected false for element not in list")
	}
	if contains(nil, "A") {
		t.Error("expected false for nil list")
	}
}

func TestTick_skipsCode(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "SKIP_ME", "createdAt": "2025-01-01"},
		{"id": 2, "code": "REDEEM_ME", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := echoRedeemServer("success", "OK")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, []string{"SKIP_ME"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
		Workers:      1,
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Fatalf("want 1 redemption (REDEEM_ME only), got %d", len(s.saved))
	}
	if s.saved[0].Code != "REDEEM_ME" {
		t.Errorf("expected REDEEM_ME saved, got %q", s.saved[0].Code)
	}
}

func TestTick_skipsCodeLogged(t *testing.T) {
	buf := captureLog(t)

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "SKIP_ME", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()
	redeemSrv := redeemServer(nil) // must not be called
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, []string{"SKIP_ME"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		BatchSize:    1,
		Workers:      1,
	}

	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, `code "SKIP_ME" skipped`) != 1 {
		t.Errorf("want skip log line; got:\n%s", out)
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
