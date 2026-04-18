package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCodesURL  = "https://kingshot.net/api/gift-codes"
	defaultRedeemURL = "https://kingshot.net/api/gift-codes/bulk-redeem"
	defaultHealthURL = "https://kingshot.net/api/health"
	defaultInterval  = 15 * time.Minute
	defaultBatchSize = 3
	maxBatchSize     = 100
	defaultWorkers   = 5
	maxWorkers       = 20
)

func defaultPaths() (dbPath, playerFile string) {
	dir, err := os.Getwd()
	if err != nil {
		return "redeemer.db", "players.txt"
	}
	return filepath.Join(dir, "redeemer.db"), filepath.Join(dir, "players.txt")
}

type Config struct {
	PlayerFile   string
	PollInterval time.Duration
	CodesURL     string
	RedeemURL    string
	HealthURL    string
	DBPath       string
	BatchSize    int
	Workers      int
}

func Load() Config {
	interval := defaultInterval
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	codesURL := defaultCodesURL
	if v := os.Getenv("CODES_URL"); v != "" {
		codesURL = v
	}

	redeemURL := defaultRedeemURL
	if v := os.Getenv("REDEEM_URL"); v != "" {
		redeemURL = v
	}

	healthURL := defaultHealthURL
	if v := os.Getenv("HEALTH_URL"); v != "" {
		healthURL = v
	}

	batchSize := defaultBatchSize
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxBatchSize {
			batchSize = n
		}
	}

	workers := defaultWorkers
	if v := os.Getenv("WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxWorkers {
			workers = n
		}
	}

	defaultDB, defaultPlayerFile := defaultPaths()

	dbPath := defaultDB
	if v := os.Getenv("DB_PATH"); v != "" {
		dbPath = v
	}

	playerFile := defaultPlayerFile
	if v := os.Getenv("PLAYER_FILE"); v != "" {
		playerFile = v
	}

	return Config{
		PlayerFile:   playerFile,
		PollInterval: interval,
		CodesURL:     codesURL,
		RedeemURL:    redeemURL,
		HealthURL:    healthURL,
		DBPath:       dbPath,
		BatchSize:    batchSize,
		Workers:      workers,
	}
}

// LoadPlayerIDs reads player IDs from a plain text file, one ID per line.
func LoadPlayerIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read player file: %w", err)
	}

	var ids []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}
