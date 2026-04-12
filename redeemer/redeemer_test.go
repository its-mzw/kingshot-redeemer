package redeemer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sseServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
}

func TestRedeem_success(t *testing.T) {
	srv := sseServer([]string{
		`{"accountId":"p1","status":"success","message":"OK","playerInfo":{"nickname":"Hero","kingdom":1}}`,
		`{"accountId":"p2","status":"success","message":"OK","playerInfo":{"nickname":"Villain","kingdom":2}}`,
	})
	defer srv.Close()

	summary, err := Redeem(context.Background(), "CODE1", []string{"p1", "p2"}, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Succeeded != 2 {
		t.Errorf("succeeded: got %d, want 2", summary.Succeeded)
	}
	if summary.Failed != 0 {
		t.Errorf("failed: got %d, want 0", summary.Failed)
	}
	if summary.Total != 2 {
		t.Errorf("total: got %d, want 2", summary.Total)
	}
	if summary.GiftCode != "CODE1" {
		t.Errorf("gift code: got %q", summary.GiftCode)
	}
}

func TestRedeem_partialFailure(t *testing.T) {
	srv := sseServer([]string{
		`{"accountId":"p1","status":"success","message":"OK"}`,
		`{"accountId":"p2","status":"failed","message":"already redeemed"}`,
	})
	defer srv.Close()

	summary, err := Redeem(context.Background(), "CODE1", []string{"p1", "p2"}, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Succeeded != 1 {
		t.Errorf("succeeded: got %d, want 1", summary.Succeeded)
	}
	if summary.Failed != 1 {
		t.Errorf("failed: got %d, want 1", summary.Failed)
	}
}

func TestRedeem_skipsProcessingEvents(t *testing.T) {
	srv := sseServer([]string{
		`{"accountId":"p1","status":"processing","message":"working..."}`,
		`{"accountId":"p1","status":"success","message":"OK"}`,
	})
	defer srv.Close()

	summary, err := Redeem(context.Background(), "CODE1", []string{"p1"}, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Results) != 1 {
		t.Errorf("results: got %d, want 1", len(summary.Results))
	}
	if summary.Results[0].Status != "success" {
		t.Errorf("status: got %q", summary.Results[0].Status)
	}
}

func TestRedeem_multilineJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\n  \"accountId\": \"p1\",\n  \"status\": \"success\",\n  \"message\": \"OK\"\n}\n\n")
		fmt.Fprintf(w, "data: {\n  \"accountId\": \"p2\",\n  \"status\": \"error\",\n  \"message\": \"Gift code expired.\"\n}\n\n")
	}))
	defer srv.Close()

	summary, err := Redeem(context.Background(), "CODE1", []string{"p1", "p2"}, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Results) != 2 {
		t.Errorf("results: got %d, want 2", len(summary.Results))
	}
	if summary.Succeeded != 1 || summary.Failed != 1 {
		t.Errorf("succeeded=%d failed=%d, want 1/1", summary.Succeeded, summary.Failed)
	}
}

func TestRedeem_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Redeem(context.Background(), "CODE1", []string{"p1"}, srv.URL)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestRedeem_contextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Redeem(ctx, "CODE1", []string{"p1"}, "http://127.0.0.1:0")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
