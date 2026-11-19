package services

import (
	"context"
	"errors"
	"testing"
)

type fakeDatabaseHealthChecker struct{ err error }

func (f fakeDatabaseHealthChecker) HealthCheck(context.Context) error { return f.err }

func TestHealthServiceCheck(t *testing.T) {
	if err := NewHealthService(fakeDatabaseHealthChecker{}).Check(context.Background()); err != nil {
		t.Fatalf("healthy database: unexpected error %v", err)
	}

	cause := errors.New("connection refused")
	err := NewHealthService(fakeDatabaseHealthChecker{err: cause}).Check(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("unhealthy database: want error wrapping %v, got %v", cause, err)
	}
}
