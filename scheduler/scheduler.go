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

		batches := chunk(remaining, cfg.BatchSize)
		workers := cfg.Workers
		if workers <= 0 {
			workers = 1
		}
		log.Printf("scheduler: redeeming %q for %d players (%d batches, %d workers)",
			code.Code, len(remaining), len(batches), workers)

		total := int64(len(remaining))
		var dispatched, activeWorkers atomic.Int64
		var res struct {
			sync.Mutex
			succeeded, expired, alreadyRedeemed, unknown int
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		for _, batch := range batches {
			batch := batch
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() {
					activeWorkers.Add(-1)
					<-sem
				}()

				active := activeWorkers.Add(1)
				d := dispatched.Add(int64(len(batch)))
				log.Printf("scheduler: %q picking up %d ids — %d/%d left, %d/%d workers busy",
					code.Code, len(batch), total-d, total, active, workers)

				summary, err := redeemer.Redeem(ctx, code.Code, batch, cfg.RedeemURL)
				if err != nil {
					log.Printf("scheduler: redeem %q: %v", code.Code, err)
					return
				}

				succ, exp, ar, unk := processSummary(s, code.Code, summary)
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

func processSummary(s store.Store, code string, summary *redeemer.Summary) (succeeded, expired, alreadyRedeemed, unknown int) {
	for _, result := range summary.Results {
		if result.Status != "success" {
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
				log.Printf("scheduler: player %s %q: %s", result.AccountID, code, result.Message)
			}
			if status != "" {
				if err := s.SaveRedemption(store.Redemption{
					PlayerID:   result.AccountID,
					Code:       code,
					RedeemedAt: summary.Timestamp,
					Status:     status,
				}); err != nil {
					log.Printf("scheduler: save player=%s code=%q: %v", result.AccountID, code, err)
				}
			}
			continue
		}
		succeeded++
		r := store.Redemption{
			PlayerID:   result.AccountID,
			Code:       code,
			RedeemedAt: summary.Timestamp,
			Status:     store.StatusSuccess,
		}
		if result.PlayerInfo != nil {
			r.Nickname = result.PlayerInfo.Nickname
			r.Kingdom = result.PlayerInfo.Kingdom
		}
		if err := s.SaveRedemption(r); err != nil {
			log.Printf("scheduler: save redemption player=%s code=%q: %v", result.AccountID, code, err)
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

func chunk(ids []string, size int) [][]string {
	var batches [][]string
	for size < len(ids) {
		ids, batches = ids[size:], append(batches, ids[:size])
	}
	return append(batches, ids)
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
