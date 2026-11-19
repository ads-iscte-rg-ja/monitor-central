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

type HealthService struct {
	db DatabaseHealthChecker
}

func NewHealthService(db DatabaseHealthChecker) *HealthService {
	return &HealthService{db: db}
}

// Check reports whether the central can serve requests, which today means
// that the database is reachable.
func (s *HealthService) Check(ctx context.Context) error {
	if err := s.db.HealthCheck(ctx); err != nil {
		return fmt.Errorf("database health check: %w", err)
	}
	return nil
}
