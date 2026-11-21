package config

import (
	"fmt"
	"strconv"
	"time"
)

const (
	// DashboardQueueWait is how long a browser request waits for a free slot
	// before it is answered 503 (ADR-0017).
	DashboardQueueWait = 2 * time.Second
	// DefaultDashboardQueryTimeout bounds the database work of one browser
	// request, below the HTTP server's 30 s WriteTimeout.
	DefaultDashboardQueryTimeout = 10 * time.Second
	maxDashboardQueryTimeout     = 25 * time.Second
)

// DashboardConfig is the bulkhead of the authenticated browser routes
// (ADR-0017): like the ingestion limit of ADR-0006, it keeps slow pages from
// taking the whole connection pool.
type DashboardConfig struct {
	// MaxInflight is the number of browser requests served at once
	// (DASHBOARD_MAX_INFLIGHT); each uses one connection at a time.
	MaxInflight int
	// QueryTimeout is the deadline of each request's database work
	// (DASHBOARD_QUERY_TIMEOUT).
	QueryTimeout time.Duration
}

// LoadDashboardConfig reads the bulkhead settings. The default is 20% of the
// pool, which with the ingestion's 60% leaves 20% for the background jobs and
// the sessions (in pools too small for that, one slot). An explicit value must
// leave at least one connection outside both limits.
func LoadDashboardConfig(maxOpenConns, ingestInflight int) (DashboardConfig, error) {
	c := DashboardConfig{
		MaxInflight:  max(1, maxOpenConns*20/100),
		QueryTimeout: DefaultDashboardQueryTimeout,
	}
	if raw := GetEnv("DASHBOARD_MAX_INFLIGHT", ""); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return c, fmt.Errorf("DASHBOARD_MAX_INFLIGHT must be a positive integer")
		}
		if n+ingestInflight >= maxOpenConns {
			return c, fmt.Errorf("DASHBOARD_MAX_INFLIGHT (%d) plus INGEST_MAX_INFLIGHT (%d) must leave a connection free in POSTGRES_MAX_OPEN_CONNS (%d)",
				n, ingestInflight, maxOpenConns)
		}
		c.MaxInflight = n
	}
	if raw := GetEnv("DASHBOARD_QUERY_TIMEOUT", ""); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Second || d > maxDashboardQueryTimeout {
			return c, fmt.Errorf("DASHBOARD_QUERY_TIMEOUT must be between 1s and %s", maxDashboardQueryTimeout)
		}
		c.QueryTimeout = d
	}
	return c, nil
}
