package services

import (
	"context"
	"fmt"

	"monitor-central/internal/models"
)

type DashboardRepository interface {
	CountHosts(
		ctx context.Context,
	) (online int, total int, err error)

	CountErrorsToday(
		ctx context.Context,
	) (total int, critical int, err error)

	CountAlerts(
		ctx context.Context,
	) (active int, firing int, err error)

	AverageLatestCPU(
		ctx context.Context,
	) (float64, error)

	ListDashboardHosts(
		ctx context.Context,
	) ([]models.DashboardHost, error)

	GetHostMetrics(
		ctx context.Context,
		hostID int64,
		metricsRange string,
	) ([]models.MetricPoint, error)
	GetLatestMetric(context.Context, int64) (*models.MetricPoint, error)
	GetBufferStatus(context.Context, int64) (*models.BufferStatus, error)
}

type DashboardService struct {
	repo DashboardRepository
}

func NewDashboardService(
	repo DashboardRepository,
) *DashboardService {
	return &DashboardService{repo: repo}
}

func (s *DashboardService) GetSummary(
	ctx context.Context,
) (*models.DashboardSummary, error) {
	hostsOnline, hostsTotal, err := s.repo.CountHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("count hosts: %w", err)
	}

	errorsToday, criticalErrorsToday, err :=
		s.repo.CountErrorsToday(ctx)
	if err != nil {
		return nil, fmt.Errorf("count errors today: %w", err)
	}

	activeAlerts, firingAlerts, err :=
		s.repo.CountAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("count alerts: %w", err)
	}

	averageCPU, err := s.repo.AverageLatestCPU(ctx)
	if err != nil {
		return nil, fmt.Errorf("average latest CPU: %w", err)
	}

	return &models.DashboardSummary{
		HostsOnline:         hostsOnline,
		HostsTotal:          hostsTotal,
		ErrorsToday:         errorsToday,
		CriticalErrorsToday: criticalErrorsToday,
		ActiveAlerts:        activeAlerts,
		FiringAlerts:        firingAlerts,
		AverageCPU:          averageCPU,
	}, nil
}

func (s *DashboardService) GetMetrics(
	ctx context.Context,
	hostID int64,
	metricsRange string,
) (*models.DashboardMetrics, error) {
	switch metricsRange {
	case "24h", "1w", "1m":
	default:
		return nil, models.ErrInvalidMetricsRange
	}
	hosts, err := s.repo.ListDashboardHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dashboard hosts: %w", err)
	}

	if len(hosts) == 0 {
		return &models.DashboardMetrics{
			Hosts: hosts,
			Range: metricsRange,
			Stats: []models.MetricPoint{},
		}, nil
	}

	var selectedHost *models.DashboardHost

	if hostID == 0 {
		selectedHost = &hosts[0]
	} else {
		for i := range hosts {
			if hosts[i].ID == hostID {
				selectedHost = &hosts[i]
				break
			}
		}
	}

	if selectedHost == nil {
		return nil, models.ErrHostNotFound
	}

	stats, err := s.repo.GetHostMetrics(
		ctx,
		selectedHost.ID,
		metricsRange,
	)
	if err != nil {
		return nil, fmt.Errorf("get host metrics: %w", err)
	}

	latest, err := s.repo.GetLatestMetric(ctx, selectedHost.ID)
	if err != nil {
		return nil, err
	}
	buffer, err := s.repo.GetBufferStatus(ctx, selectedHost.ID)
	if err != nil {
		return nil, fmt.Errorf("get buffer status: %w", err)
	}
	return &models.DashboardMetrics{
		Latest:       latest,
		Hosts:        hosts,
		SelectedHost: selectedHost,
		Range:        metricsRange,
		Stats:        stats,
		Buffer:       buffer,
	}, nil
}
