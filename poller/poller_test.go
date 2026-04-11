package poller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeHealthServer(t *testing.T, status string, dbCheck, serverCheck string, apiErr *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := healthResponse{Status: status, Error: apiErr}
		h.Checks.Database = dbCheck
		h.Checks.Server = serverCheck
		json.NewEncoder(w).Encode(h)
	}))
}

func TestCheckHealth_healthy(t *testing.T) {
	srv := makeHealthServer(t, "healthy", "ok", "ok", nil)
	defer srv.Close()

	if err := CheckHealth(srv.URL); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckHealth_unhealthyWithError(t *testing.T) {
	msg := "database connection lost"
	srv := makeHealthServer(t, "unhealthy", "error", "ok", &msg)
	defer srv.Close()

	err := CheckHealth(srv.URL)
	if err == nil {
		t.Error("expected error for unhealthy status")
	}
}

func TestCheckHealth_unhealthyNoError(t *testing.T) {
	srv := makeHealthServer(t, "unhealthy", "error", "error", nil)
	defer srv.Close()

	err := CheckHealth(srv.URL)
	if err == nil {
		t.Error("expected error for unhealthy status")
	}
}

func TestCheckHealth_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := CheckHealth(srv.URL); err == nil {
		t.Error("expected error for 503 response")
	}
}

func makeServer(t *testing.T, codes []GiftCode) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{}
		resp.Status = "success"
		resp.Data.GiftCodes = codes
		resp.Data.Total = len(codes)
		resp.Data.ActiveCount = len(codes)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetchActiveCodes_success(t *testing.T) {
	codes := []GiftCode{
		{ID: 1, Code: "CODE1", CreatedAt: "2025-01-01"},
		{ID: 2, Code: "CODE2", CreatedAt: "2025-01-02"},
	}
	srv := makeServer(t, codes)
	defer srv.Close()

	got, err := FetchActiveCodes(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d codes, want 2", len(got))
	}
	if got[0].Code != "CODE1" {
		t.Errorf("first code: got %q, want CODE1", got[0].Code)
	}
}

func TestFetchActiveCodes_empty(t *testing.T) {
	srv := makeServer(t, nil)
	defer srv.Close()

	got, err := FetchActiveCodes(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestFetchActiveCodes_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchActiveCodes(srv.URL)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestFetchActiveCodes_invalidURL(t *testing.T) {
	_, err := FetchActiveCodes("not-a-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}
