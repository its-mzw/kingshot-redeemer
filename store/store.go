package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StatusSuccess        = "success"
	StatusExpired        = "expired"
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

	if version < 1 {
		if err := migrate1(db); err != nil {
			return err
		}
	}
	if version < 2 {
		if err := migrate2(db); err != nil {
			return err
		}
	}

	return nil
}

// migration 1: initial schema
func migrate1(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS redemptions (
			player_id   TEXT NOT NULL,
			code        TEXT NOT NULL,
			redeemed_at DATETIME NOT NULL,
			nickname    TEXT NOT NULL DEFAULT '',
			kingdom     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (player_id, code)
		);
		PRAGMA user_version = 1;
	`)
	return err
}

// migration 2: add status column
func migrate2(db *sql.DB) error {
	_, err := db.Exec(`
		ALTER TABLE redemptions ADD COLUMN status TEXT NOT NULL DEFAULT 'success';
		PRAGMA user_version = 2;
	`)
	return err
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
