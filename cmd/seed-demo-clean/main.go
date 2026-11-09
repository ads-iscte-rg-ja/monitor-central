package main

import (
	"context"
	"log"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"os"
	"time"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("demo cleanup failed: %v", err)
		os.Exit(1)
	}
}
func run(logger *log.Logger) error {
	if err := config.LoadEnv(".env"); err != nil {
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
	result, err := db.ExecContext(ctx, "DELETE FROM hosts WHERE demo=TRUE")
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	logger.Printf("Removed %d demo hosts and their monitoring history.", count)
	return nil
}
