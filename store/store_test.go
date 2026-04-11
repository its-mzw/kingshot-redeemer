package store

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIsRedeemed_notRedeemed(t *testing.T) {
	s := newTestStore(t)
	ok, err := s.IsRedeemed("player1", "CODE1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected not redeemed")
	}
}

func TestSaveRedemption_and_IsRedeemed(t *testing.T) {
	s := newTestStore(t)
	r := Redemption{
		PlayerID:   "player1",
		Code:       "CODE1",
		RedeemedAt: time.Now(),
		Nickname:   "Hero",
		Kingdom:    42,
	}

	if err := s.SaveRedemption(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	ok, err := s.IsRedeemed("player1", "CODE1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected redeemed")
	}
}

func TestSaveRedemption_idempotent(t *testing.T) {
	s := newTestStore(t)
	r := Redemption{
		PlayerID:   "player1",
		Code:       "CODE1",
		RedeemedAt: time.Now(),
	}

	if err := s.SaveRedemption(r); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.SaveRedemption(r); err != nil {
		t.Fatalf("second save (should be no-op): %v", err)
	}
}

func TestIsRedeemed_differentPlayer(t *testing.T) {
	s := newTestStore(t)
	r := Redemption{PlayerID: "player1", Code: "CODE1", RedeemedAt: time.Now()}
	if err := s.SaveRedemption(r); err != nil {
		t.Fatal(err)
	}

	ok, err := s.IsRedeemed("player2", "CODE1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("player2 should not be marked as redeemed")
	}
}

func TestIsRedeemed_differentCode(t *testing.T) {
	s := newTestStore(t)
	r := Redemption{PlayerID: "player1", Code: "CODE1", RedeemedAt: time.Now()}
	if err := s.SaveRedemption(r); err != nil {
		t.Fatal(err)
	}

	ok, err := s.IsRedeemed("player1", "CODE2")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("CODE2 should not be marked as redeemed")
	}
}
