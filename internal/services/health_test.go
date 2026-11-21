package services

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeDatabaseHealthChecker struct{ err error }

func (f fakeDatabaseHealthChecker) HealthCheck(context.Context) error { return f.err }

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestReadinessChecksTheDatabase(t *testing.T) {
	if r := NewHealthService(fakeDatabaseHealthChecker{}, nil).Ready(context.Background()); !r.Ready() {
		t.Fatalf("healthy database: %+v", r)
	}
	cause := errors.New("connection refused")
	r := NewHealthService(fakeDatabaseHealthChecker{err: cause}, nil).Ready(context.Background())
	if r.Ready() || !errors.Is(r.DatabaseErr, cause) {
		t.Fatalf("unhealthy database: %+v", r)
	}
}

// MN2: a led job that stops beating is stale after twice its interval, not before.
func TestJobHeartbeats(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	beats := NewJobHeartbeats(clock.Now)
	beats.Register("metric_alerts", 15*time.Second)
	beats.Register("retention", time.Hour)
	beats.Starting("unregistered", true) // ignored: no interval known
	beats.Finished("unregistered")

	beats.Starting("retention", false) // led elsewhere: never stale here
	beats.Starting("metric_alerts", true)
	clock.Advance(30 * time.Second)
	if stale := beats.Stale(); len(stale) != 0 {
		t.Fatalf("stale at exactly twice the interval: %v", stale)
	}
	clock.Advance(time.Millisecond)
	if stale := beats.Stale(); !reflect.DeepEqual(stale, []string{"metric_alerts"}) {
		t.Fatalf("blocked job not reported: %v", stale)
	}
	health := NewHealthService(fakeDatabaseHealthChecker{}, beats)
	if r := health.Ready(context.Background()); r.Ready() || len(r.StaleJobs) != 1 {
		t.Fatalf("readiness with a blocked job: %+v", r)
	}

	beats.Finished("metric_alerts") // the iteration ends: a beat
	if stale := beats.Stale(); len(stale) != 0 {
		t.Fatalf("job beat but still stale: %v", stale)
	}
	clock.Advance(2 * time.Hour)
	beats.Starting("metric_alerts", false) // lost the lead
	if stale := beats.Stale(); len(stale) != 0 {
		t.Fatalf("a job led elsewhere was reported: %v", stale)
	}
	// Taking the lead starts the clock now, not at the last beat.
	beats.Starting("retention", true)
	if stale := beats.Stale(); len(stale) != 0 {
		t.Fatalf("newly led job reported at once: %v", stale)
	}

	var none *JobHeartbeats // jobs disabled (web role): every call is a no-op
	none.Register("x", time.Second)
	none.Starting("x", true)
	none.Finished("x")
	if none.Stale() != nil {
		t.Fatal("nil heartbeats must report nothing")
	}
}
