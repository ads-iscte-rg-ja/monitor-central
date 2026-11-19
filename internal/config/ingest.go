package config

import (
	"fmt"
	"strconv"
)

// IngestConfig bounds how much of the database the ingestion may use (ADR-0006).
type IngestConfig struct {
	// MaxInflight is the number of ingestion requests served at once. Each uses
	// at most one database connection at a time, so the rest of the pool stays
	// free for the dashboard, sessions and background jobs.
	MaxInflight int
}

// LoadIngestConfig reads INGEST_MAX_INFLIGHT; the default is 60% of the pool.
func LoadIngestConfig(maxOpenConns int) (*IngestConfig, error) {
	if maxOpenConns < 2 {
		return nil, fmt.Errorf("POSTGRES_MAX_OPEN_CONNS must be at least 2 to reserve connections outside ingestion")
	}
	limit := max(1, maxOpenConns*60/100)
	if raw := GetEnv("INGEST_MAX_INFLIGHT", ""); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v >= maxOpenConns {
			return nil, fmt.Errorf("INGEST_MAX_INFLIGHT must be between 1 and POSTGRES_MAX_OPEN_CONNS-1 (%d)", maxOpenConns-1)
		}
		limit = v
	}
	return &IngestConfig{MaxInflight: limit}, nil
}
