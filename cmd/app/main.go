package main

import (
	"log"
	"os"

	"monitor-central/internal/server"
)

func main() {
	// NOTE: should initilize in server package?
	logger := log.New(os.Stderr, "", log.LstdFlags)
	logger.Println("Starting Monitor central...")
	s, err := server.New(logger)
	if err != nil {
		log.Fatalf("Error initializing the server: %v", err)
	}

	if err := s.Run(); err != nil {
		logger.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
