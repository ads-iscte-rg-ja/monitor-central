package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	// HostHealthInterval is how often each host with an endpoint is probed, and
	// the deadline of one iteration of the job (ADR-0013).
	HostHealthInterval = 30 * time.Second
	// HostHealthTick is how often the job wakes up to probe the hosts now due.
	HostHealthTick      = 5 * time.Second
	HostHealthTimeout   = 5 * time.Second
	HostHealthFreshness = 90 * time.Second
	// DefaultHostHealthConcurrency fits 1,000 hosts with half of them timing out:
	// 500 × 5 s every 30 s keep ≈ 83 probes busy (ADR-0013).
	DefaultHostHealthConcurrency = 128
	maxHostHealthConcurrency     = 1024
)

// HealthConfig holds the settings of the host health probes.
type HealthConfig struct {
	// Concurrency is the maximum number of probes in flight (HEALTH_CONCURRENCY).
	Concurrency int
}

func LoadHealthConfig() (HealthConfig, error) {
	c := HealthConfig{Concurrency: DefaultHostHealthConcurrency}
	if value := os.Getenv("HEALTH_CONCURRENCY"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > maxHostHealthConcurrency {
			return c, fmt.Errorf("HEALTH_CONCURRENCY must be an integer between 1 and %d", maxHostHealthConcurrency)
		}
		c.Concurrency = n
	}
	return c, nil
}
