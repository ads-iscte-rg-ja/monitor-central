package repositories

import (
	"context"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
)

type ErrorLogsRepository struct {
	db *database.DB
}

func NewErrorLogsRepository(
	db *database.DB,
) *ErrorLogsRepository {
	return &ErrorLogsRepository{db: db}
}

// errorLogFilter is the WHERE clause of the error log page. It is a constant:
// an empty filter ($1 = 0, or ”) switches its condition off, and PostgreSQL
// plans each execution with the values bound, so the unused conditions fold
// away and the indexes still apply. $1 host, $2 level, $3 application, $4 the
// ILIKE pattern.
const errorLogFilter = `($1::bigint = 0 OR el.host_id = $1)
   AND ($2::text = '' OR el.level = $2)
   AND ($3::text = '' OR el.app_name = $3)
   AND ($4::text = '' OR el.message ILIKE $4)`

// countErrorLogs stops counting at $5 rows (ADR-0017), so its cost is bounded
// by the cap and not by the size of the table.
const countErrorLogs = `SELECT COUNT(*) FROM (
	SELECT 1 FROM error_logs el WHERE ` + errorLogFilter + `
	LIMIT $5) AS capped`

const listErrorLogs = `SELECT
		el.id,
		el.host_id,
		h.name AS host_name,
		el.app_name,
		el.environment,
		el.level,
		el.message,
		el.stack_trace,
		COALESCE(el.metadata, 'null'::jsonb) AS metadata,
		el.created_at
	FROM error_logs el
	JOIN hosts h ON h.id = el.host_id
	WHERE ` + errorLogFilter + `
	ORDER BY el.created_at DESC, el.id DESC
	LIMIT $5
	OFFSET $6`

// ListErrorLogs returns one page of errors and the number of matching errors,
// counted up to models.ErrorLogCountCap+1: a larger count means "more than the
// cap" (ADR-0017).
func (r *ErrorLogsRepository) ListErrorLogs(
	ctx context.Context,
	filters models.ErrorLogFilters,
) ([]models.ErrorLogListItem, int, error) {
	pattern := ""
	if filters.Search != "" {
		pattern = "%" + filters.Search + "%"
	}
	args := []any{filters.HostID, filters.Level, filters.AppName, pattern}

	var total int
	if err := r.db.QueryRowContext(
		ctx,
		countErrorLogs,
		append(args, models.ErrorLogCountCap+1)...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	errorLogs := []models.ErrorLogListItem{}
	if err := r.db.Select(
		ctx,
		&errorLogs,
		listErrorLogs,
		append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)...,
	); err != nil {
		return nil, 0, err
	}

	return errorLogs, total, nil
}

func (r *ErrorLogsRepository) ListErrorLogHosts(
	ctx context.Context,
) ([]models.DashboardHost, error) {
	hosts := []models.DashboardHost{}

	err := r.db.Select(
		ctx,
		&hosts,
		`SELECT id, name, hostname, status
		 FROM hosts
		 ORDER BY name, id`,
	)
	if err != nil {
		return nil, err
	}

	return hosts, nil
}

func (r *ErrorLogsRepository) ListErrorLogAppNames(
	ctx context.Context,
) ([]string, error) {
	// A loose index scan (ADR-0017): one lookup in idx_error_logs_app_name
	// per distinct name, instead of reading every error to find a handful.
	rows, err := r.db.QueryContext(
		ctx,
		`WITH RECURSIVE apps(name) AS (
			(SELECT app_name FROM error_logs ORDER BY app_name LIMIT 1)
			UNION ALL
			SELECT (
				SELECT e.app_name FROM error_logs e
				WHERE e.app_name > apps.name
				ORDER BY e.app_name
				LIMIT 1
			)
			FROM apps
			WHERE apps.name IS NOT NULL
		)
		SELECT name FROM apps WHERE name IS NOT NULL ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	appNames := []string{}

	for rows.Next() {
		var appName string

		if err := rows.Scan(&appName); err != nil {
			return nil, err
		}

		appNames = append(appNames, appName)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return appNames, nil
}
