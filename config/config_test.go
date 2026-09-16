package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_defaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("SESSION_TOKEN", "abc123")
	t.Cleanup(os.Clearenv)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PollInterval != defaultInterval {
		t.Errorf("interval: got %v, want %v", cfg.PollInterval, defaultInterval)
	}
	if cfg.CodesURL != defaultCodesURL {
		t.Errorf("codes url: got %q, want %q", cfg.CodesURL, defaultCodesURL)
	}
	if cfg.RedeemURL != defaultRedeemURL {
		t.Errorf("redeem url: got %q, want %q", cfg.RedeemURL, defaultRedeemURL)
	}
	if filepath.Base(cfg.DBPath) != "redeemer.db" {
		t.Errorf("db path: got %q, want filename redeemer.db", cfg.DBPath)
	}
	if filepath.Base(cfg.PlayerFile) != "players.txt" {
		t.Errorf("player file: got %q, want filename players.txt", cfg.PlayerFile)
	}
	if cfg.Workers != defaultWorkers {
		t.Errorf("workers: got %d, want %d", cfg.Workers, defaultWorkers)
	}
}

func TestLoad_envOverride(t *testing.T) {
	os.Setenv("POLL_INTERVAL", "10m")
	os.Setenv("CODES_URL", "http://custom/codes")
	os.Setenv("REDEEM_URL", "http://custom/redeem")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("PLAYER_FILE", "/tmp/players.txt")
	os.Setenv("SKIPPING_FILE", "/tmp/skipping_codes.txt")
	os.Setenv("SESSION_TOKEN", "abc123")
	t.Cleanup(os.Clearenv)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PollInterval != 10*time.Minute {
		t.Errorf("interval: got %v, want 10m", cfg.PollInterval)
	}
	if cfg.CodesURL != "http://custom/codes" {
		t.Errorf("codes url: got %q", cfg.CodesURL)
	}
	if cfg.RedeemURL != "http://custom/redeem" {
		t.Errorf("redeem url: got %q", cfg.RedeemURL)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("db path: got %q", cfg.DBPath)
	}
	if cfg.PlayerFile != "/tmp/players.txt" {
		t.Errorf("player file: got %q", cfg.PlayerFile)
	}
	if cfg.SkippingFile != "/tmp/skipping_codes.txt" {
		t.Errorf("skipping file: got %q", cfg.SkippingFile)
	}
}

func TestLoad_workersDefault(t *testing.T) {
	os.Clearenv()
	os.Setenv("SESSION_TOKEN", "abc123")
	t.Cleanup(os.Clearenv)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workers != defaultWorkers {
		t.Errorf("workers default: got %d, want %d", cfg.Workers, defaultWorkers)
	}
}

func TestLoad_workersEnvOverride(t *testing.T) {
	os.Setenv("WORKERS", "10")
	os.Setenv("SESSION_TOKEN", "abc123")
	t.Cleanup(os.Clearenv)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workers != 10 {
		t.Errorf("workers: got %d, want 10", cfg.Workers)
	}
}

func TestLoad_sessionTokenEnvOverride(t *testing.T) {
	os.Setenv("SESSION_TOKEN", "abc123")
	t.Cleanup(os.Clearenv)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionToken != "abc123" {
		t.Errorf("session token: got %q, want %q", cfg.SessionToken, "abc123")
	}
}

func TestLoad_missingSessionTokenErrors(t *testing.T) {
	os.Clearenv()
	t.Cleanup(os.Clearenv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when SESSION_TOKEN is unset, got nil")
	}
}

func TestLoadPlayerIDs_plainText(t *testing.T) {
	f, err := os.CreateTemp("", "players*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("111\n222\n333\n")
	f.Close()

	ids, err := LoadPlayerIDs(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 || ids[0] != "111" || ids[2] != "333" {
		t.Errorf("got %v", ids)
	}
}

func TestLoadPlayerIDs_missingFile(t *testing.T) {
	_, err := LoadPlayerIDs("/nonexistent/path/players.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
