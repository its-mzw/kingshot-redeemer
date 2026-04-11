package scheduler

import (
	"context"
	"kingshot-redeemer/config"
	"kingshot-redeemer/poller"
	"kingshot-redeemer/redeemer"
	"kingshot-redeemer/store"
	"log"
	"strings"
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
		remaining := filterUnredeemed(s, playerIDs, code.Code)
		if len(remaining) == 0 {
			log.Printf("scheduler: code %q already redeemed by all players", code.Code)
			continue
		}

		log.Printf("scheduler: redeeming %q for %d players", code.Code, len(remaining))
		for _, batch := range chunk(remaining, cfg.BatchSize) {
			summary, err := redeemer.Redeem(ctx, code.Code, batch, cfg.RedeemURL)
			if err != nil {
				log.Printf("scheduler: redeem %q: %v", code.Code, err)
				continue
			}
			log.Printf("scheduler: code %q batch=%d — succeeded=%d failed=%d", code.Code, len(batch), summary.Succeeded, summary.Failed)
			processSummary(s, code.Code, summary)
		}
	}
}

func processSummary(s store.Store, code string, summary *redeemer.Summary) {
	for _, result := range summary.Results {
		if result.Status != "success" {
			log.Printf("scheduler: player %s code %q: %s", result.AccountID, code, result.Message)
			if strings.Contains(strings.ToLower(result.Message), "expired") {
				if err := s.SaveRedemption(store.Redemption{
					PlayerID:   result.AccountID,
					Code:       code,
					RedeemedAt: summary.Timestamp,
					Status:     store.StatusExpired,
				}); err != nil {
					log.Printf("scheduler: save expired code player=%s code=%q: %v", result.AccountID, code, err)
				}
			}
			continue
		}
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
