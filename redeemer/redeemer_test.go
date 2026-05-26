package redeemer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func successResponse() map[string]any {
	return map[string]any{
		"status":  "success",
		"data":    map[string]any{"redemption": "SUCCESS", "autoAdded": false, "accountUpdated": false},
		"message": "Gift code redeemed successfully. Please check your mail in the game.",
	}
}

func TestRedeem_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer srv.Close()

	result, err := Redeem(context.Background(), "KS0524", "12345", srv.URL, "token123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("status: got %q, want %q", result.Status, "success")
	}
	if result.PlayerID != "12345" {
		t.Errorf("playerID: got %q, want %q", result.PlayerID, "12345")
	}
}

func TestRedeem_sendsSessionCookie(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer srv.Close()

	_, err := Redeem(context.Background(), "CODE", "p1", srv.URL, "my-session-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotCookie, "my-session-token") {
		t.Errorf("cookie: got %q, want to contain session token", gotCookie)
	}
}

func TestRedeem_sendsCorrectBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer srv.Close()

	_, err := Redeem(context.Background(), "KS0524", "17976339", srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["giftCode"] != "KS0524" {
		t.Errorf("giftCode: got %q, want %q", gotBody["giftCode"], "KS0524")
	}
	if gotBody["playerId"] != "17976339" {
		t.Errorf("playerId: got %q, want %q", gotBody["playerId"], "17976339")
	}
	_, hasAccountIds := gotBody["accountIds"]
	if hasAccountIds {
		t.Error("body must not contain accountIds (old bulk field)")
	}
}

func TestRedeem_500_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"data":    nil,
			"message": "Request failed with status code 429",
			"meta":    map[string]any{"code": "INTERNAL_ERROR"},
		})
	}))
	defer srv.Close()

	_, err := Redeem(context.Background(), "CODE", "p1", srv.URL, "token")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestRedeem_405_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	_, err := Redeem(context.Background(), "CODE", "p1", srv.URL, "token")
	if err == nil {
		t.Error("expected error for 405 response")
	}
}

func TestRedeem_contextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Redeem(ctx, "CODE", "p1", "http://127.0.0.1:0", "token")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
