package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"monitor-central/internal/models"
)

type fakeErrorLogsRepository struct {
	listErrorsFn func(
		ctx context.Context,
		filters models.ErrorLogFilters,
	) ([]models.ErrorLogListItem, int, error)

	listHostsFn func(
		ctx context.Context,
	) ([]models.DashboardHost, error)

	listAppNamesFn func(
		ctx context.Context,
	) ([]string, error)
}

func (f *fakeErrorLogsRepository) ListErrorLogs(
	ctx context.Context,
	filters models.ErrorLogFilters,
) ([]models.ErrorLogListItem, int, error) {
	return f.listErrorsFn(ctx, filters)
}

func (f *fakeErrorLogsRepository) ListErrorLogHosts(
	ctx context.Context,
) ([]models.DashboardHost, error) {
	return f.listHostsFn(ctx)
}

func (f *fakeErrorLogsRepository) ListErrorLogAppNames(
	ctx context.Context,
) ([]string, error) {
	return f.listAppNamesFn(ctx)
}

func TestGetErrorLogsPageSuccess(t *testing.T) {
	createdAt := time.Date(
		2026, time.August, 5, 17, 30, 0, 0, time.UTC,
	)

	repo := &fakeErrorLogsRepository{
		listErrorsFn: func(
			ctx context.Context,
			filters models.ErrorLogFilters,
		) ([]models.ErrorLogListItem, int, error) {
			if filters.Page != 1 {
				t.Errorf("expected page 1, got %d", filters.Page)
			}

			if filters.PageSize != 20 {
				t.Errorf(
					"expected page size 20, got %d",
					filters.PageSize,
				)
			}

			return []models.ErrorLogListItem{
				{
					ID:          1,
					HostID:      1,
					HostName:    "Server Test",
					AppName:     "payments-api",
					Environment: "production",
					Level:       "ERROR",
					Message:     "database connection failed",
					CreatedAt:   createdAt,
				},
			}, 21, nil
		},

		listHostsFn: func(
			ctx context.Context,
		) ([]models.DashboardHost, error) {
			return []models.DashboardHost{
				{
					ID:   1,
					Name: "Server Test",
				},
			}, nil
		},

		listAppNamesFn: func(
			ctx context.Context,
		) ([]string, error) {
			return []string{"payments-api"}, nil
		},
	}

	service := NewErrorLogsService(repo)

	page, err := service.GetPage(
		context.Background(),
		models.ErrorLogFilters{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(page.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(page.Errors))
	}

	if page.TotalErrors != 21 {
		t.Errorf(
			"expected 21 total errors, got %d",
			page.TotalErrors,
		)
	}

	if page.Page != 1 {
		t.Errorf("expected page 1, got %d", page.Page)
	}

	if page.PageSize != 20 {
		t.Errorf(
			"expected page size 20, got %d",
			page.PageSize,
		)
	}

	if page.TotalPages != 2 {
		t.Errorf(
			"expected 2 total pages, got %d",
			page.TotalPages,
		)
	}

	if len(page.Hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(page.Hosts))
	}

	if len(page.AppNames) != 1 {
		t.Errorf(
			"expected 1 app name, got %d",
			len(page.AppNames),
		)
	}
}

func TestGetErrorLogsPageInvalidLevel(t *testing.T) {
	repositoryCalled := false

	repo := &fakeErrorLogsRepository{
		listErrorsFn: func(
			ctx context.Context,
			filters models.ErrorLogFilters,
		) ([]models.ErrorLogListItem, int, error) {
			repositoryCalled = true
			return []models.ErrorLogListItem{}, 0, nil
		},

		listHostsFn: func(
			ctx context.Context,
		) ([]models.DashboardHost, error) {
			repositoryCalled = true
			return []models.DashboardHost{}, nil
		},

		listAppNamesFn: func(
			ctx context.Context,
		) ([]string, error) {
			repositoryCalled = true
			return []string{}, nil
		},
	}

	service := NewErrorLogsService(repo)

	_, err := service.GetPage(
		context.Background(),
		models.ErrorLogFilters{
			Level: "DEBUG",
		},
	)

	if !errors.Is(err, models.ErrInvalidErrorLevel) {
		t.Fatalf(
			"expected ErrInvalidErrorLevel, got %v",
			err,
		)
	}

	if repositoryCalled {
		t.Error(
			"repository should not be called with an invalid level",
		)
	}
}
