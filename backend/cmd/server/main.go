package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"confectioner-backend/internal/api"
	"confectioner-backend/internal/bot"
	"confectioner-backend/internal/config"
	"confectioner-backend/internal/db"
	"confectioner-backend/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	log.Printf("[Confectioner CRM] Starting server on :%s (DEV_MODE=%v)", cfg.Port, cfg.DevMode)

	// Connect to PostgreSQL with retry
	var store *db.Store
	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		store, err = db.NewStore(ctx, cfg.DatabaseURL)
		if err == nil {
			break
		}
		log.Printf("[DB] Waiting for PostgreSQL (attempt %d/15): %v", attempt, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		log.Fatalf("[DB] Failed to initialize PostgreSQL: %v", err)
	}
	defer store.Close()

	// Connect to S3 (MinIO) with retry
	var s3 *storage.S3Storage
	for attempt := 1; attempt <= 15; attempt++ {
		s3, err = storage.NewS3Storage(ctx, cfg)
		if err == nil {
			break
		}
		log.Printf("[S3] Waiting for MinIO/S3 (attempt %d/15): %v", attempt, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		log.Fatalf("[S3] Failed to initialize S3 storage: %v", err)
	}

	if cfg.DevMode && cfg.MaxBotToken == "" {
		if err := store.SeedDemoData(ctx, s3); err != nil {
			log.Printf("[Seed] Warning seeding demo data: %v", err)
		} else {
			log.Printf("[Seed] Demo clients, messages, and S3 cake photos ready")
		}
	}

	hub := api.NewSSEHub()

	botClient, err := bot.NewClient(cfg)
	if err != nil {
		log.Fatalf("[MAX Bot] Failed to initialize MAX client: %v", err)
	}

	botService := bot.NewService(cfg, botClient, store, s3, hub)
	go botService.StartPolling(ctx)

	srv := api.NewServer(cfg, store, s3, botService, hub)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[HTTP] API listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[HTTP] ListenAndServe error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[Confectioner CRM] Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

