package scheduler

import (
	"context"
	"fmt"
	"kingshot-redeemer/config"
	"kingshot-redeemer/poller"
	"kingshot-redeemer/redeemer"
	"kingshot-redeemer/store"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Run starts the poll-redeem loop. Blocks until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, s store.Store) {
	log.Printf("scheduler: starting, interval=%s", cfg.PollInterval)
	tick(ctx, cfg, s)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("scheduler: stopping")
			return
		case <-ticker.C:
			tick(ctx, cfg, s)
		}
	}
}

func tick(ctx context.Context, cfg config.Config, s store.Store) {
	playerIDs, err := config.LoadPlayerIDs(cfg.PlayerFile)
	if err != nil {
		log.Printf("scheduler: load players: %v", err)
		return
	}
	if len(playerIDs) == 0 {
		log.Println("scheduler: no player IDs configured")
		return
	}

	skippingCodes, err := config.LoadSkippedCodes(cfg.SkippingFile)
	if err != nil {
		log.Printf("scheduler: load skipping codes: %v", err)
		return
	}

	if err := poller.CheckHealth(cfg.HealthURL); err != nil {
		log.Printf("scheduler: %v — skipping tick", err)
		return
	}

	codes, err := poller.FetchActiveCodes(cfg.CodesURL)
	if err != nil {
		log.Printf("scheduler: fetch codes: %v", err)
		return
	}
	if len(codes) == 0 {
		log.Println("scheduler: no active codes")
		return
	}

	for _, code := range codes {
		if contains(skippingCodes, code.Code) {
			log.Printf("scheduler: code %q skipped", code.Code)
			continue
		}

		remaining := filterUnredeemed(s, playerIDs, code.Code)
		if len(remaining) == 0 {
			log.Printf("scheduler: code %q already redeemed by all players", code.Code)
			continue
		}

		workers := cfg.Workers
		if workers <= 0 {
			workers = 1
		}
		log.Printf("scheduler: redeeming %q for %d players (%d workers)",
			code.Code, len(remaining), workers)

		var res struct {
			sync.Mutex
			succeeded, expired, alreadyRedeemed, unknown int
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		dispatched := atomic.Int64{}
		total := int64(len(remaining))

		for _, playerID := range remaining {
			playerID := playerID
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				n := dispatched.Add(1)
				ts := time.Now().UTC()

				result, err := redeemer.Redeem(ctx, code.Code, playerID, cfg.RedeemURL, cfg.SessionToken)
				if err != nil {
					log.Printf("scheduler: redeem [%d/%d] player=%s %q: %v", n, total, playerID, code.Code, err)
					return
				}

				succ, exp, ar, unk := processResult(s, code.Code, result, ts)
				res.Lock()
				res.succeeded += succ
				res.expired += exp
				res.alreadyRedeemed += ar
				res.unknown += unk
				res.Unlock()
			}()
		}
		wg.Wait()

		failed := res.expired + res.alreadyRedeemed + res.unknown
		log.Printf("scheduler: %q done — %d succeeded, %d failed%s",
			code.Code, res.succeeded, failed, failureSuffix(res.expired, res.alreadyRedeemed, res.unknown))
	}
}

func processResult(s store.Store, code string, result *redeemer.Result, ts time.Time) (succeeded, expired, alreadyRedeemed, unknown int) {
	if result.Status == "success" {
		succeeded++
		if err := s.SaveRedemption(store.Redemption{
			PlayerID:   result.PlayerID,
			Code:       code,
			RedeemedAt: ts,
			Status:     store.StatusSuccess,
		}); err != nil {
			log.Printf("scheduler: save redemption player=%s code=%q: %v", result.PlayerID, code, err)
		}
		return
	}

	msg := strings.ToLower(result.Message)
	var status string
	switch {
	case strings.Contains(msg, "expired"):
		status = store.StatusExpired
		expired++
	case strings.Contains(msg, "already redeemed"):
		status = store.StatusAlreadyRedeemed
		alreadyRedeemed++
	default:
		unknown++
		log.Printf("scheduler: player %s %q: %s", result.PlayerID, code, result.Message)
	}
	if status != "" {
		if err := s.SaveRedemption(store.Redemption{
			PlayerID:   result.PlayerID,
			Code:       code,
			RedeemedAt: ts,
			Status:     status,
		}); err != nil {
			log.Printf("scheduler: save player=%s code=%q: %v", result.PlayerID, code, err)
		}
	}
	return
}

func failureSuffix(expired, alreadyRedeemed, unknown int) string {
	var parts []string
	if expired > 0 {
		parts = append(parts, fmt.Sprintf("%d expired", expired))
	}
	if alreadyRedeemed > 0 {
		parts = append(parts, fmt.Sprintf("%d already_redeemed", alreadyRedeemed))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown", unknown))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func filterUnredeemed(s store.Store, playerIDs []string, code string) []string {
	var remaining []string
	for _, id := range playerIDs {
		ok, err := s.IsRedeemed(id, code)
		if err != nil {
			log.Printf("scheduler: check redeemed player=%s code=%q: %v", id, code, err)
			continue
		}
		if !ok {
			remaining = append(remaining, id)
		}
	}
	return remaining
}

func contains(skippingCodes []string, code string) bool {
	for _, c := range skippingCodes {
		if c == code {
			return true
		}
	}
	return false
}
