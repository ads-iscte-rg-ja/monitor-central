package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/handlers"
	"monitor-central/internal/observability"
	"monitor-central/internal/services"
)

type okDatabase struct{}

func (okDatabase) HealthCheck(context.Context) error { return nil }

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// MN2: a job that blocks while this instance leads it puts /health/ready in 503
// within twice its interval; /health/live keeps answering. No sleeps: the clock
// is controlled.
func TestBlockedJobMakesTheInstanceNotReady(t *testing.T) {
	clock := &testClock{now: time.Unix(1_000_000, 0)}
	s := &Server{
		elector:  &fakeLeader{leads: map[string]bool{"metric_alerts": true}},
		metrics:  observability.NewMetrics(nil, observability.Options{}),
		logger:   log.New(io.Discard, "", 0),
		jobBeats: services.NewJobHeartbeats(clock.Now),
	}
	s.jobBeats.Register("metric_alerts", 15*time.Second)
	s.jobBeats.Register("notifications", 5*time.Second)
	health := handlers.NewHealthHandler(services.NewHealthService(okDatabase{}, s.jobBeats), s.logger)
	handler := httpHandler(config.RoleWorker, nil, health.Live, health.Ready)
	get := func(path string) (int, string) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code, w.Body.String()
	}

	// One normal iteration of each job; notifications is led by another instance.
	if err := s.runJob(context.Background(), "notifications", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.runJob(context.Background(), "metric_alerts", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	// The next iteration blocks.
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = s.runJob(context.Background(), "metric_alerts", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	clock.Advance(30 * time.Second)
	if code, body := get("/health/ready"); code != http.StatusOK {
		t.Fatalf("not yet two intervals: %d %s", code, body)
	}
	clock.Advance(time.Second)
	code, body := get("/health/ready")
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "metric_alerts") || strings.Contains(body, "notifications") {
		t.Fatalf("blocked job: %d %s", code, body)
	}
	if code, _ := get("/health"); code != http.StatusServiceUnavailable {
		t.Fatal("/health must follow /health/ready")
	}
	if code, _ := get("/health/live"); code != http.StatusOK {
		t.Fatal("liveness must not depend on the jobs")
	}

	close(release)
	<-done
	if code, body := get("/health/ready"); code != http.StatusOK {
		t.Fatalf("the job finished but the instance is still not ready: %d %s", code, body)
	}
}
