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

	log.Printf("starting redeemer (interval=%s, db=%s, players=%s)", cfg.PollInterval, cfg.DBPath, cfg.PlayerFile)

	s, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler.Run(ctx, cfg, s)
}
