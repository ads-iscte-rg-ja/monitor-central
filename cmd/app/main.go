package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	"monitor-central/internal/config"
	"monitor-central/internal/observability"
	"monitor-central/internal/server"
)

func main() {
	slogger, logger, obs, err := setup(os.Stderr, ".env")
	if err != nil {
		log.Fatal(err)
	}
	logger.Println("Starting Monitor central...")
	s, err := server.New(logger, slogger, obs)
	if err != nil {
		logger.Printf("Error initializing the server: %v", err)
		os.Exit(1)
	}
	if err := s.Run(); err != nil {
		logger.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

// setup loads the environment and builds the structured logger (ADR-0004)
// before anything else, so every later line uses the configured format.
func setup(w io.Writer, envFile string) (*slog.Logger, *log.Logger, *config.ObservabilityConfig, error) {
	if err := config.LoadEnv(envFile); err != nil {
		return nil, nil, nil, fmt.Errorf("load environment: %w", err)
	}
	obs, err := config.LoadObservabilityConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("observability configuration: %w", err)
	}
	slogger, logger, err := observability.NewLogger(w, obs.LogFormat, obs.LogLevel)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("logging configuration: %w", err)
	}
	return slogger, logger, obs, nil
}
