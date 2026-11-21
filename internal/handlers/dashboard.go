package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"monitor-central/internal/models"
	"monitor-central/internal/templating/pages"
)

// dashboardService is what the dashboard needs (ADR-0014).
type dashboardService interface {
	GetSummary(ctx context.Context) (*models.DashboardSummary, error)
	GetMetrics(ctx context.Context, hostID int64, metricsRange string) (*models.DashboardMetrics, error)
}

type DashboardHandler struct {
	service dashboardService
	tb      *pages.PageBox
	logger  *log.Logger
}

func NewDashboardHandler(
	service dashboardService,
	tb *pages.PageBox,
	logger *log.Logger,
) *DashboardHandler {
	return &DashboardHandler{
		service: service,
		tb:      tb,
		logger:  logger,
	}
}

func (h *DashboardHandler) GetDashboard(
	w http.ResponseWriter,
	r *http.Request,
) {
	summary, err := h.service.GetSummary(r.Context())
	if err != nil {
		h.logger.Printf(
			"Failed to load dashboard summary: %v",
			err,
		)

		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	allSystemsOperational :=
		summary.HostsTotal > 0 &&
			summary.HostsOnline == summary.HostsTotal &&
			summary.FiringAlerts == 0

	h.tb.RenderPage(
		"dashboard",
		map[string]interface{}{
			"HostsOnline":           summary.HostsOnline,
			"HostsTotal":            summary.HostsTotal,
			"ErrorsToday":           summary.ErrorsToday,
			"CriticalErrorsToday":   summary.CriticalErrorsToday,
			"ActiveAlerts":          summary.ActiveAlerts,
			"FiringAlerts":          summary.FiringAlerts,
			"AverageCPU":            summary.AverageCPU,
			"AllSystemsOperational": allSystemsOperational,
		},
		w,
		r,
	)
}

func (h *DashboardHandler) GetMetrics(
	w http.ResponseWriter,
	r *http.Request,
) {
	hostID, err := optionalID(r.URL.Query().Get("host_id"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	metricsRange := r.URL.Query().Get("range")

	if metricsRange == "" {
		metricsRange = "24h"
	}

	metrics, err := h.service.GetMetrics(
		r.Context(),
		hostID,
		metricsRange,
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			models.ErrInvalidMetricsRange,
		):
			http.Error(
				w,
				"Invalid metrics range",
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrHostNotFound):
			http.Error(
				w,
				"Host not found",
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to load dashboard metrics: %v",
				err,
			)

			http.Error(
				w,
				http.StatusText(
					http.StatusInternalServerError,
				),
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		h.logger.Printf(
			"Failed to encode dashboard metrics: %v",
			err,
		)
	}
}

func (h *DashboardHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.GetSummary(r.Context())
	if err != nil {
		h.logger.Printf("dashboard summary: %v", err)
		http.Error(w, "Summary unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(summary)
}
