package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"monitor-central/internal/services"
)

type fakeDatabaseHealthChecker struct{ err error }

func (f fakeDatabaseHealthChecker) HealthCheck(context.Context) error { return f.err }

func TestHealthCheck(t *testing.T) {
	for _, test := range []struct {
		name   string
		dbErr  error
		status int
	}{
		{"database reachable", nil, http.StatusOK},
		{"database unavailable", errors.New("connection refused"), http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewHealthHandler(services.NewHealthService(fakeDatabaseHealthChecker{err: test.dbErr}))
			w := httptest.NewRecorder()
			h.HealthCheck(w, httptest.NewRequest(http.MethodGet, "/health", nil))

			if w.Code != test.status {
				t.Fatalf("want status %d, got %d", test.status, w.Code)
			}
			if test.status != http.StatusOK {
				return
			}
			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["status"] != "healthy" || body["time"] == "" {
				t.Fatalf("unexpected body %v", body)
			}
		})
	}
}
