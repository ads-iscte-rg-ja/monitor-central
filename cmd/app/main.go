package main

import (
	"log"
	"os"

	"monitor-central/internal/config"
	"monitor-central/internal/observability"
	"monitor-central/internal/server"
)

func main() {
	if err := config.LoadEnv(".env"); err != nil {
		log.Fatalf("load environment: %v", err)
	}
	obs, err := config.LoadObservabilityConfig()
	if err != nil {
		log.Fatalf("observability configuration: %v", err)
	}
	slogger, logger, err := observability.NewLogger(os.Stderr, obs.LogFormat, obs.LogLevel)
	if err != nil {
		log.Fatalf("logging configuration: %v", err)
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
