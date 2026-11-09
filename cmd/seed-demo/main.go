package main

import (
	"context"
	"log"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/seed"
	"monitor-central/internal/utils/apikey"
	"os"
	"time"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("demo seed failed: %v", err)
		os.Exit(1)
	}
}
func run(logger *log.Logger) error {
	if err := config.LoadEnv(".env"); err != nil {
		return err
	}
	secret, err := config.LoadAPIKeySecret()
	if err != nil {
		return err
	}
	hasher, err := apikey.New(secret)
	if err != nil {
		return err
	}
	cfg, err := config.LoadDBConfig()
	if err != nil {
		return err
	}
	db, err := database.New(cfg, logger)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.CheckSchema(ctx); err != nil {
		return err
	}
	if err := db.CheckAPIKeySecret(ctx, hasher); err != nil {
		return err
	}
	if err := seed.Demo(ctx, db, hasher); err != nil {
		return err
	}
	logger.Println("Demo hosts, samples, logs, and dashboard-only rules ready.")
	return nil
}
