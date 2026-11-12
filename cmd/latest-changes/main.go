package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hainuoli/latest-changes/internal/action"
	"github.com/hainuoli/latest-changes/internal/config"
	"github.com/hainuoli/latest-changes/internal/github"
)

const runTimeout = 90 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("Latest Changes failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := github.NewClient(cfg.Token)
	if err != nil {
		return err
	}
	notesClient := client
	if cfg.NotesToken != "" {
		notesClient, err = github.NewClient(cfg.NotesToken)
		if err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	return action.NewRunner(client, notesClient).Run(ctx, cfg)
}
