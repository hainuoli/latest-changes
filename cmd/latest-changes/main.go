package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hainuoli/latest-changes/internal/app"
	"github.com/hainuoli/latest-changes/internal/config"
	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/queue"
	"github.com/hainuoli/latest-changes/internal/webhook"
)

const defaultQueueCapacity = 1000

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("Latest Changes stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := github.NewClient(cfg.ClientID, cfg.PrivateKey)
	if err != nil {
		return err
	}
	spool, err := queue.NewQueue(cfg.DataDir, defaultQueueCapacity)
	if err != nil {
		return err
	}
	service := app.NewService(client)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- spool.Run(workerCtx, service.ProcessEvent)
	}()
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           webhook.NewHandler(cfg.WebhookSecret, version, spool),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      8 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		cancelWorker()
		<-workerDone
		return err
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	slog.Info("Latest Changes started", "address", listener.Addr().String())
	var result error
	workerExited := false
	select {
	case <-ctx.Done():
	case err := <-workerDone:
		workerExited = true
		result = fmt.Errorf("webhook worker stopped: %w", err)
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		if result == nil {
			result = err
		}
	}
	cancelWorker()
	if !workerExited {
		select {
		case err := <-workerDone:
			if result == nil {
				result = err
			}
		case <-shutdownCtx.Done():
			if result == nil {
				result = shutdownCtx.Err()
			}
		}
	}
	return result
}
