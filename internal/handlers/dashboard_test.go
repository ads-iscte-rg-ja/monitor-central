package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"monitor-central/internal/models"
)

type fakeDashboard struct {
	summary    *models.DashboardSummary
	summaryErr error
	metricsErr error
	hostID     int64
	rangeName  string
}

func (f *fakeDashboard) GetSummary(context.Context) (*models.DashboardSummary, error) {
	return f.summary, f.summaryErr
}
func (f *fakeDashboard) GetMetrics(_ context.Context, hostID int64, r string) (*models.DashboardMetrics, error) {
	f.hostID, f.rangeName = hostID, r
	if f.metricsErr != nil {
		return nil, f.metricsErr
	}
	return &models.DashboardMetrics{Range: r, Hosts: []models.DashboardHost{{ID: hostID}}}, nil
}

func TestDashboardPageAndSummary(t *testing.T) {
	svc := &fakeDashboard{summary: &models.DashboardSummary{HostsOnline: 3, HostsTotal: 3, AverageCPU: 12.5}}
	h := NewDashboardHandler(svc, realPages(t), discard())
	// The placeholder must have an empty value: dashboard.js sends the selected
	// value as host_id, and its text made every first request fail with 400.
	wantStatus(t, call(t, "GET", "/dashboard", "/dashboard", nil, h.GetDashboard), http.StatusOK, `<option value="">Loading hosts`)

	w := call(t, "GET", "/api/v1/dashboard/summary", "/api/v1/dashboard/summary", nil, h.GetSummary)
	var got models.DashboardSummary
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.HostsOnline != 3 {
		t.Fatalf("summary %d %q", w.Code, w.Body.String())
	}

	svc.summaryErr = errStore
	wantStatus(t, call(t, "GET", "/dashboard", "/dashboard", nil, h.GetDashboard), http.StatusInternalServerError, "Internal Server Error")
	wantStatus(t, call(t, "GET", "/api/v1/dashboard/summary", "/api/v1/dashboard/summary", nil, h.GetSummary), http.StatusInternalServerError, "Summary unavailable")
}

func TestDashboardMetricsDefaultsAndErrors(t *testing.T) {
	svc := &fakeDashboard{}
	h := NewDashboardHandler(svc, nil, discard())
	const path = "/api/v1/dashboard/metrics"
	w := call(t, "GET", path, path+"?host_id=4", nil, h.GetMetrics)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || svc.hostID != 4 || svc.rangeName != "24h" {
		t.Fatalf("status %d host %d range %q", w.Code, svc.hostID, svc.rangeName)
	}
	_ = call(t, "GET", path, path+"?range=7d", nil, h.GetMetrics)
	if svc.hostID != 0 || svc.rangeName != "7d" {
		t.Fatalf("host %d range %q", svc.hostID, svc.rangeName)
	}
	wantStatus(t, call(t, "GET", path, path+"?host_id=abc", nil, h.GetMetrics), http.StatusBadRequest, "Invalid host ID")
	for err, status := range map[error]int{models.ErrInvalidMetricsRange: 400, models.ErrHostNotFound: 404, errStore: 500} {
		svc.metricsErr = err
		if w := call(t, "GET", path, path, nil, h.GetMetrics); w.Code != status {
			t.Fatalf("%v: %d, want %d", err, w.Code, status)
		}
	}
}
