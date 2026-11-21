package services

import (
	"context"
	"fmt"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"
)

type ErrorLogsRepository interface {
	ListErrorLogs(
		ctx context.Context,
		filters models.ErrorLogFilters,
	) ([]models.ErrorLogListItem, int, error)

	ListErrorLogHosts(
		ctx context.Context,
	) ([]models.DashboardHost, error)

	ListErrorLogAppNames(
		ctx context.Context,
	) ([]string, error)
}

type ErrorLogsService struct {
	repo ErrorLogsRepository
	loc  *time.Location // renders error times (APP_TIMEZONE, ADR-0014)
}

// NewErrorLogsService renders times in loc (the default zone if nil).
func NewErrorLogsService(
	repo ErrorLogsRepository,
	loc *time.Location,
) *ErrorLogsService {
	if loc == nil {
		loc = config.DefaultLocation()
	}
	return &ErrorLogsService{repo: repo, loc: loc}
}

func (s *ErrorLogsService) GetPage(
	ctx context.Context,
	filters models.ErrorLogFilters,
) (*models.ErrorLogsPage, error) {
	if filters.Level != "" && !models.ValidLevel(filters.Level) { // empty means all levels
		return nil, models.ErrInvalidErrorLevel
	}
	if filters.Page <= 0 {
		filters.Page = 1
	}

	if filters.PageSize <= 0 {
		filters.PageSize = 20
	}

	errorsList, totalErrors, err :=
		s.repo.ListErrorLogs(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list error logs: %w", err)
	}

	for i := range errorsList {
		errorsList[i].CreatedAtDisplay =
			errorsList[i].CreatedAt.In(
				s.loc,
			).Format(
				"02 Jan 2006, 15:04",
			)
	}

	hosts, err := s.repo.ListErrorLogHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list error log hosts: %w", err)
	}

	appNames, err := s.repo.ListErrorLogAppNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list error log app names: %w", err)
	}

	totalPages := 0
	if totalErrors > 0 {
		totalPages =
			(totalErrors + filters.PageSize - 1) / filters.PageSize
	}

	return &models.ErrorLogsPage{
		Errors:      errorsList,
		Hosts:       hosts,
		AppNames:    appNames,
		TotalErrors: totalErrors,
		Page:        filters.Page,
		PageSize:    filters.PageSize,
		TotalPages:  totalPages,
		Filters:     filters,
	}, nil
}
