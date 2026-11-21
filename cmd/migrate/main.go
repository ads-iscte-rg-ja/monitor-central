package main

import (
	"context"
	"log"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/utils/apikey"
	"os"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("migration failed: %v", err)
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
	timeout, err := config.LoadMigrateTimeout()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.Migrate(ctx, hasher); err != nil {
		return err
	}
	logger.Println("Schema ready. Legacy sessions, if any, have been invalidated.")
	return nil
}
