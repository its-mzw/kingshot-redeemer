package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_defaults(t *testing.T) {
	os.Clearenv()
	cfg := Load()

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
	os.Setenv("PLAYER_FILE", "/tmp/players.json")
	t.Cleanup(os.Clearenv)

	cfg := Load()

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
	if cfg.PlayerFile != "/tmp/players.json" {
		t.Errorf("player file: got %q", cfg.PlayerFile)
	}
}

func TestLoad_workersDefault(t *testing.T) {
	os.Clearenv()
	cfg := Load()
	if cfg.Workers != defaultWorkers {
		t.Errorf("workers default: got %d, want %d", cfg.Workers, defaultWorkers)
	}
}

func TestLoad_workersEnvOverride(t *testing.T) {
	os.Setenv("WORKERS", "10")
	t.Cleanup(os.Clearenv)
	cfg := Load()
	if cfg.Workers != 10 {
		t.Errorf("workers: got %d, want 10", cfg.Workers)
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
