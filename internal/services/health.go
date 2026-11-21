package services

import (
	"context"
	"fmt"
)

// DatabaseHealthChecker is the only capability the health check needs from
// the database. Declaring it here keeps services independent of the
// database package (dependency inversion); *database.DB satisfies it.
type DatabaseHealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// StaleJobs reports the led background jobs that stopped beating; *JobHeartbeats
// satisfies it.
type StaleJobs interface {
	Stale() []string
}

type HealthService struct {
	db   DatabaseHealthChecker
	jobs StaleJobs
}

// NewHealthService builds the readiness check; jobs may be nil (no background jobs).
func NewHealthService(db DatabaseHealthChecker, jobs StaleJobs) *HealthService {
	return &HealthService{db: db, jobs: jobs}
}

// Readiness is the result of the self-test behind /health/ready (ADR-0010).
type Readiness struct {
	DatabaseErr error
	StaleJobs   []string
}

// Ready reports whether this instance can do its work.
func (r Readiness) Ready() bool { return r.DatabaseErr == nil && len(r.StaleJobs) == 0 }

// Ready runs the self-test: the database answers and every job this instance
// leads beat within twice its interval.
func (s *HealthService) Ready(ctx context.Context) Readiness {
	var result Readiness
	if err := s.db.HealthCheck(ctx); err != nil {
		result.DatabaseErr = fmt.Errorf("database health check: %w", err)
	}
	if s.jobs != nil {
		result.StaleJobs = s.jobs.Stale()
	}
	return result
}
