package config

import (
	"fmt"
	"time"
)

const (
	// DefaultMigrateTimeout bounds cmd/migrate. A migration that rebuilds an
	// index or fills a table reads it whole (ADR-0017): migration 8 took 4 min 12 s
	// over 30 days of 1,000 hosts (143 GB, 2 vCPU). 30 min leaves room for that;
	// raise MIGRATE_TIMEOUT for more data.
	DefaultMigrateTimeout = 30 * time.Minute
	maxMigrateTimeout     = 24 * time.Hour
)

// LoadMigrateTimeout reads MIGRATE_TIMEOUT (1m to 24h).
func LoadMigrateTimeout() (time.Duration, error) {
	raw := GetEnv("MIGRATE_TIMEOUT", "")
	if raw == "" {
		return DefaultMigrateTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Minute || d > maxMigrateTimeout {
		return 0, fmt.Errorf("MIGRATE_TIMEOUT must be between 1m and %s", maxMigrateTimeout)
	}
	return d, nil
}
