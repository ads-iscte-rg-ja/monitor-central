package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"monitor-central/internal/models"
)

type fakeDashboardRepository struct {
	hostsOnline         int
	hostsTotal          int
	errorsToday         int
	criticalErrorsToday int
	activeAlerts        int
	firingAlerts        int
	averageCPU          float64
	hosts               []models.DashboardHost
	stats               []models.MetricPoint
	requestedHostID     int64
	requestedRange      string
}

func (f *fakeDashboardRepository) CountHosts(
	ctx context.Context,
) (int, int, error) {
	return f.hostsOnline, f.hostsTotal, nil
}

func (f *fakeDashboardRepository) CountErrorsToday(
	ctx context.Context,
) (int, int, error) {
	return f.errorsToday, f.criticalErrorsToday, nil
}

func (f *fakeDashboardRepository) CountAlerts(
	ctx context.Context,
) (int, int, error) {
	return f.activeAlerts, f.firingAlerts, nil
}

func (f *fakeDashboardRepository) AverageLatestCPU(
	ctx context.Context,
) (float64, error) {
	return f.averageCPU, nil
}

func (f *fakeDashboardRepository) ListDashboardHosts(
	ctx context.Context,
) ([]models.DashboardHost, error) {
	return f.hosts, nil
}

func (f *fakeDashboardRepository) GetHostMetrics(
	ctx context.Context,
	hostID int64,
	metricsRange string,
) ([]models.MetricPoint, error) {
	f.requestedHostID = hostID
	f.requestedRange = metricsRange

	return f.stats, nil
}

func (f *fakeDashboardRepository) GetLatestMetric(context.Context, int64) (*models.MetricPoint, error) {
	if len(f.stats) == 0 {
		return nil, nil
	}
	return &f.stats[len(f.stats)-1], nil
}

func TestGetDashboardSummarySuccess(t *testing.T) {
	repo := &fakeDashboardRepository{
		hostsOnline:         4,
		hostsTotal:          5,
		errorsToday:         3,
		criticalErrorsToday: 1,
		activeAlerts:        2,
		firingAlerts:        1,
		averageCPU:          42.4,
	}

	service := NewDashboardService(repo)

	summary, err := service.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("did not expect an error: %v", err)
	}

	if summary.HostsOnline != 4 {
		t.Errorf(
			"expected 4 online hosts, got %d",
			summary.HostsOnline,
		)
	}

	if summary.HostsTotal != 5 {
		t.Errorf(
			"expected 5 total hosts, got %d",
			summary.HostsTotal,
		)
	}

	if summary.ErrorsToday != 3 {
		t.Errorf(
			"expected 3 errors today, got %d",
			summary.ErrorsToday,
		)
	}

	if summary.CriticalErrorsToday != 1 {
		t.Errorf(
			"expected 1 critical error today, got %d",
			summary.CriticalErrorsToday,
		)
	}

	if summary.ActiveAlerts != 2 {
		t.Errorf(
			"expected 2 active alerts, got %d",
			summary.ActiveAlerts,
		)
	}

	if summary.FiringAlerts != 1 {
		t.Errorf(
			"expected 1 firing alert, got %d",
			summary.FiringAlerts,
		)
	}

	if summary.AverageCPU != 42.4 {
		t.Errorf(
			"expected average CPU 42.4, got %f",
			summary.AverageCPU,
		)
	}
}

func TestGetDashboardMetricsSuccess(t *testing.T) {
	collectedAt := time.Date(
		2026,
		8,
		5,
		18,
		0,
		0,
		0,
		time.UTC,
	)

	repo := &fakeDashboardRepository{
		hosts: []models.DashboardHost{
			{
				ID:       1,
				Name:     "Server One",
				Hostname: "server-one.local",
				Status:   "online",
			},
			{
				ID:       2,
				Name:     "Server Two",
				Hostname: "server-two.local",
				Status:   "online",
			},
		},
		stats: []models.MetricPoint{
			{
				CPUPercent:  40,
				RAMUsedMB:   4096,
				RAMTotalMB:  8192,
				DiskUsedGB:  120,
				DiskTotalGB: 500,
				CollectedAt: collectedAt,
			},
		},
	}

	service := NewDashboardService(repo)

	metrics, err := service.GetMetrics(
		context.Background(),
		2,
		"24h",
	)
	if err != nil {
		t.Fatalf("did not expect an error: %v", err)
	}

	if metrics.SelectedHost == nil {
		t.Fatal("expected a selected host")
	}

	if metrics.SelectedHost.ID != 2 {
		t.Errorf(
			"expected selected host 2, got %d",
			metrics.SelectedHost.ID,
		)
	}

	if metrics.Range != "24h" {
		t.Errorf(
			"expected range 24h, got %s",
			metrics.Range,
		)
	}

	if len(metrics.Stats) != 1 {
		t.Fatalf(
			"expected 1 metric point, got %d",
			len(metrics.Stats),
		)
	}

	if repo.requestedHostID != 2 {
		t.Errorf(
			"expected repository host ID 2, got %d",
			repo.requestedHostID,
		)
	}

	if repo.requestedRange != "24h" {
		t.Errorf(
			"expected repository range 24h, got %s",
			repo.requestedRange,
		)
	}
}

func TestGetDashboardMetricsInvalidRange(t *testing.T) {
	tests := []string{
		"",
		"12h",
		"1y",
	}

	for _, metricsRange := range tests {
		t.Run(metricsRange, func(t *testing.T) {
			repo := &fakeDashboardRepository{
				hosts: []models.DashboardHost{
					{
						ID:     1,
						Name:   "Server One",
						Status: "online",
					},
				},
			}

			service := NewDashboardService(repo)

			_, err := service.GetMetrics(
				context.Background(),
				1,
				metricsRange,
			)

			if !errors.Is(
				err,
				models.ErrInvalidMetricsRange,
			) {
				t.Errorf(
					"expected ErrInvalidMetricsRange, got %v",
					err,
				)
			}
		})
	}
}
