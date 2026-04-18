package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	StatusSuccess         = "success"
	StatusExpired         = "expired"
	StatusAlreadyRedeemed = "already_redeemed"
)

// Store tracks redeemed codes per player.
type Store interface {
	IsRedeemed(playerID, code string) (bool, error)
	SaveRedemption(r Redemption) error
	Close() error
}

// Redemption represents a recorded code redemption attempt.
type Redemption struct {
	PlayerID   string
	Code       string
	RedeemedAt time.Time
	Status     string
	Nickname   string
	Kingdom    int
}

type sqliteStore struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at the given path and returns a Store.
// Use ":memory:" for in-memory databases (e.g. tests).
func New(path string) (Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	for i, entry := range entries {
		migrationVersion := i + 1
		if version >= migrationVersion {
			continue
		}
		sql, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := db.Exec(string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func (s *sqliteStore) IsRedeemed(playerID, code string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM redemptions WHERE player_id = ? AND code = ?)`,
		playerID, code,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check redemption: %w", err)
	}
	return exists, nil
}

func (s *sqliteStore) SaveRedemption(r Redemption) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO redemptions (player_id, code, redeemed_at, status, nickname, kingdom)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.PlayerID, r.Code, r.RedeemedAt.UTC(), r.Status, r.Nickname, r.Kingdom,
	)
	if err != nil {
		return fmt.Errorf("save redemption: %w", err)
	}
	return nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
