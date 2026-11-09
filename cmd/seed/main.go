// Seed provisions the operator-managed administrator. There is no signup route.
package main

import (
	"context"
	"log"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/seed"
	"os"
	"time"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("seed failed: %v", err)
		os.Exit(1)
	}
}
func run(logger *log.Logger) error {
	if err := config.LoadEnv(".env"); err != nil {
		return err
	}
	admin, err := config.LoadAdminConfig()
	if err != nil {
		return err
	}
	dbConfig, err := config.LoadDBConfig()
	if err != nil {
		return err
	}
	db, err := database.New(dbConfig, logger)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.CheckSchema(ctx); err != nil {
		return err
	}
	if _, err := seed.Admin(ctx, db, admin); err != nil {
		return err
	}
	logger.Println("Administrator provisioned from environment; existing sessions revoked.")
	return nil
}
