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

// jsonRedeemServer returns a per-player JSON response with the given status and message.
func jsonRedeemServer(status, message string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var data any
		if status == "success" {
			data = map[string]any{"redemption": "SUCCESS", "autoAdded": false, "accountUpdated": false}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":  status,
			"data":    data,
			"message": message,
		})
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

	redeemSrv := jsonRedeemServer("success", "Gift code redeemed successfully.")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "test-token",
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Errorf("saved: got %d, want 1", len(s.saved))
	}
	if s.saved[0].PlayerID != "p1" || s.saved[0].Code != "CODE1" {
		t.Errorf("saved: got %+v", s.saved[0])
	}
}

func TestTick_sendsCookieOnRedeem(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "success",
			"data":    map[string]any{"redemption": "SUCCESS"},
			"message": "ok",
		})
	}))
	defer srv.Close()

	healthSrv := healthServer()
	defer healthSrv.Close()
	codesSrv := codesServer([]map[string]any{{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"}})
	defer codesSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    srv.URL,
		SessionToken: "secret-session-token",
	}

	tick(context.Background(), cfg, newMockStore())

	if !strings.Contains(gotCookie, "secret-session-token") {
		t.Errorf("cookie: got %q, want to contain session token", gotCookie)
	}
}

func TestTick_skipsAlreadyRedeemed(t *testing.T) {
	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	redeemSrv := jsonRedeemServer("success", "ok") // must not be called
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
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("error", "Gift code expired.")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("success", "ok") // must not be called
	defer redeemSrv.Close()

	s := newMockStore()
	s.redeemed["p1|KS0408"] = true

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("error", "Gift code already redeemed.")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("success", "ok") // must not be called
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
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("error", "Internal server error.")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "token",
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

	redeemSrv := jsonRedeemServer("error", "Gift code expired.")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		SessionToken: "token",
	}

	tick(context.Background(), cfg, s)

	if len(s.saved) != 1 {
		t.Fatalf("expected expired code to be saved, got %d entries", len(s.saved))
	}
	if s.saved[0].Status != store.StatusExpired {
		t.Errorf("status: got %q, want %q", s.saved[0].Status, store.StatusExpired)
	}
}

func TestTick_parallelPlayers(t *testing.T) {
	const playerDelay = 80 * time.Millisecond
	const numPlayers = 9
	const numWorkers = 3

	healthSrv := healthServer()
	defer healthSrv.Close()

	codesSrv := codesServer([]map[string]any{
		{"id": 1, "code": "CODE1", "createdAt": "2025-01-01"},
	})
	defer codesSrv.Close()

	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(playerDelay)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "success",
			"data":    map[string]any{"redemption": "SUCCESS"},
			"message": "ok",
		})
	}))
	defer slowSrv.Close()

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
		RedeemURL:    slowSrv.URL,
		Workers:      numWorkers,
		SessionToken: "token",
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

	// Sequential: 9 * playerDelay. Parallel with 3 workers: ceil(9/3) * playerDelay = 3 * playerDelay.
	// Allow generous 5x to avoid flakiness.
	maxExpected := 5 * playerDelay
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
	redeemSrv := jsonRedeemServer("success", "Gift code redeemed successfully.")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		Workers: 3, SessionToken: "token",
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

	if countLines(out, `redeeming "CODE1" for 9 players (3 workers)`) != 1 {
		t.Errorf("missing start line; got:\n%s", out)
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
	redeemSrv := jsonRedeemServer("error", "Gift code expired.")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		Workers: 3, SessionToken: "token",
	}
	tick(context.Background(), cfg, newMockStore())
	out := buf.String()

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
	redeemSrv := jsonRedeemServer("error", "Internal server error.")
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: redeemSrv.URL,
		Workers: 1, SessionToken: "token",
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

	var mu sync.Mutex
	responses := []map[string]any{
		{"status": "error", "message": "Gift code expired."},
		{"status": "error", "message": "Gift code already redeemed."},
		{"status": "success", "message": "Gift code redeemed successfully."},
	}
	idx := 0
	mixedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		res := responses[idx%len(responses)]
		idx++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var data any
		if res["status"] == "success" {
			data = map[string]any{"redemption": "SUCCESS"}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":  res["status"],
			"data":    data,
			"message": res["message"],
		})
	}))
	defer mixedSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1", "p2", "p3"}),
		SkippingFile: skippingFile(t, nil),
		HealthURL:    healthSrv.URL, CodesURL: codesSrv.URL, RedeemURL: mixedSrv.URL,
		Workers: 1, SessionToken: "token", // sequential so response order is deterministic
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

func TestProcessResult(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("success saves", func(t *testing.T) {
		s := newMockStore()
		result := &redeemer.Result{PlayerID: "p1", Status: "success", Message: "ok"}
		succ, exp, ar, unk := processResult(s, "CODE1", result, ts)
		if succ != 1 || exp != 0 || ar != 0 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 1/0/0/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 {
			t.Fatalf("want 1 saved, got %d", len(s.saved))
		}
		r := s.saved[0]
		if r.Status != store.StatusSuccess || r.PlayerID != "p1" || r.Code != "CODE1" {
			t.Errorf("saved: %+v", r)
		}
	})

	t.Run("expired saved and counted", func(t *testing.T) {
		s := newMockStore()
		result := &redeemer.Result{PlayerID: "p1", Status: "error", Message: "Gift code expired."}
		succ, exp, ar, unk := processResult(s, "CODE1", result, ts)
		if succ != 0 || exp != 1 || ar != 0 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/1/0/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 || s.saved[0].Status != store.StatusExpired {
			t.Errorf("want expired saved, got %+v", s.saved)
		}
	})

	t.Run("already redeemed saved and counted", func(t *testing.T) {
		s := newMockStore()
		result := &redeemer.Result{PlayerID: "p1", Status: "error", Message: "Gift code already redeemed."}
		succ, exp, ar, unk := processResult(s, "CODE1", result, ts)
		if succ != 0 || exp != 0 || ar != 1 || unk != 0 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/0/1/0", succ, exp, ar, unk)
		}
		if len(s.saved) != 1 || s.saved[0].Status != store.StatusAlreadyRedeemed {
			t.Errorf("want already_redeemed saved, got %+v", s.saved)
		}
	})

	t.Run("unknown error not saved", func(t *testing.T) {
		s := newMockStore()
		result := &redeemer.Result{PlayerID: "p1", Status: "error", Message: "Internal server error."}
		succ, exp, ar, unk := processResult(s, "CODE1", result, ts)
		if succ != 0 || exp != 0 || ar != 0 || unk != 1 {
			t.Errorf("counts: got %d/%d/%d/%d, want 0/0/0/1", succ, exp, ar, unk)
		}
		if len(s.saved) != 0 {
			t.Errorf("unknown error should not be saved, got %+v", s.saved)
		}
	})

	t.Run("mixed results", func(t *testing.T) {
		s := newMockStore()
		results := []*redeemer.Result{
			{PlayerID: "p1", Status: "success", Message: "ok"},
			{PlayerID: "p2", Status: "error", Message: "Gift code expired."},
			{PlayerID: "p3", Status: "error", Message: "Gift code already redeemed."},
			{PlayerID: "p4", Status: "error", Message: "Unexpected."},
		}
		var totalSucc, totalExp, totalAr, totalUnk int
		for _, r := range results {
			succ, exp, ar, unk := processResult(s, "CODE1", r, ts)
			totalSucc += succ
			totalExp += exp
			totalAr += ar
			totalUnk += unk
		}
		if totalSucc != 1 || totalExp != 1 || totalAr != 1 || totalUnk != 1 {
			t.Errorf("counts: got %d/%d/%d/%d, want 1/1/1/1", totalSucc, totalExp, totalAr, totalUnk)
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

	redeemSrv := jsonRedeemServer("success", "ok")
	defer redeemSrv.Close()

	s := newMockStore()
	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, []string{"SKIP_ME"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		Workers:      1,
		SessionToken: "token",
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
	redeemSrv := jsonRedeemServer("success", "ok") // must not be called
	defer redeemSrv.Close()

	cfg := config.Config{
		PlayerFile:   playerFile(t, []string{"p1"}),
		SkippingFile: skippingFile(t, []string{"SKIP_ME"}),
		PollInterval: time.Minute,
		HealthURL:    healthSrv.URL,
		CodesURL:     codesSrv.URL,
		RedeemURL:    redeemSrv.URL,
		Workers:      1,
		SessionToken: "token",
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
