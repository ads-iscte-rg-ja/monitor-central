package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"monitor-central/internal/services"
)

type fakeDatabaseHealthChecker struct{ err error }

func (f fakeDatabaseHealthChecker) HealthCheck(context.Context) error { return f.err }

type fakeStaleJobs []string

func (f fakeStaleJobs) Stale() []string { return f }

func TestHealthEndpoints(t *testing.T) {
	for _, test := range []struct {
		name     string
		dbErr    error
		stale    fakeStaleJobs
		status   int
		database string
	}{
		{"ready", nil, nil, http.StatusOK, ""},
		{"database unavailable", errors.New("connection refused"), nil, http.StatusServiceUnavailable, "unavailable"},
		{"stale job", nil, fakeStaleJobs{"metric_alerts"}, http.StatusServiceUnavailable, "ok"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewHealthHandler(services.NewHealthService(fakeDatabaseHealthChecker{err: test.dbErr}, test.stale), log.New(io.Discard, "", 0))
			w := httptest.NewRecorder()
			h.Ready(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			if w.Code != test.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("want status %d, got %d", test.status, w.Code)
			}
			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if test.status == http.StatusOK {
				if body["status"] != "ready" || body["time"] == "" {
					t.Fatalf("unexpected body %v", body)
				}
			} else if body["status"] != "not ready" || body["database"] != test.database || (len(test.stale) > 0) != (body["stale_jobs"] != nil) {
				t.Fatalf("unexpected body %v", body)
			}

			// Liveness has no dependencies: it answers even when not ready.
			live := httptest.NewRecorder()
			h.Live(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
			if live.Code != http.StatusOK {
				t.Fatalf("live: %d", live.Code)
			}
		})
	}
}
