package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"monitor-central/internal/services"
)

// readinessService is the self-test behind /health/ready (ADR-0014).
type readinessService interface {
	Ready(ctx context.Context) services.Readiness
}

type HealthHandler struct {
	service readinessService
	logger  *log.Logger
}

func NewHealthHandler(service readinessService, logger *log.Logger) *HealthHandler {
	return &HealthHandler{service: service, logger: logger}
}

// Live answers while the process serves HTTP; it has no dependencies, so a
// restart is the only thing that would fix it failing (ADR-0010).
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, map[string]any{"status": "live"})
}

// Ready is the self-test: database plus the heartbeats of the jobs this
// instance leads. /health answers the same.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	readiness := h.service.Ready(r.Context())
	if readiness.Ready() {
		writeHealth(w, http.StatusOK, map[string]any{"status": "ready", "time": time.Now().Format(time.RFC3339)})
		return
	}
	body := map[string]any{"status": "not ready", "database": "ok"}
	if readiness.DatabaseErr != nil {
		h.logger.Printf("readiness: %v", readiness.DatabaseErr)
		body["database"] = "unavailable"
	}
	if len(readiness.StaleJobs) > 0 {
		h.logger.Printf("readiness: stale background jobs %v", readiness.StaleJobs)
		body["stale_jobs"] = readiness.StaleJobs
	}
	writeHealth(w, http.StatusServiceUnavailable, body)
}

func writeHealth(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The status line is already sent; a failed write means the client went
	// away and there is nothing left to report to it.
	_ = json.NewEncoder(w).Encode(body)
}
