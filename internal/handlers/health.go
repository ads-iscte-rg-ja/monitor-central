package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"monitor-central/internal/services"
)

type HealthHandler struct {
	service *services.HealthService
}

func NewHealthHandler(service *services.HealthService) *HealthHandler {
	return &HealthHandler{service: service}
}

func (h *HealthHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Check(r.Context()); err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// The status line is already sent; a failed write means the client went
	// away and there is nothing left to report to it.
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}
