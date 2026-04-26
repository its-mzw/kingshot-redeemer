package main

import (
	"context"
	"kingshot-redeemer/config"
	"kingshot-redeemer/scheduler"
	"kingshot-redeemer/store"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := config.Load()

	log.Printf("starting redeemer (interval=%s, workers=%d, db=%s, players=%s, skipping=%s)", cfg.PollInterval, cfg.Workers, cfg.DBPath, cfg.PlayerFile, cfg.SkippingFile)

	s, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler.Run(ctx, cfg, s)
}
