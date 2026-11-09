package repositories

import (
	"context"
	"fmt"
	"strings"

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

func (r *ErrorLogsRepository) ListErrorLogs(
	ctx context.Context,
	filters models.ErrorLogFilters,
) ([]models.ErrorLogListItem, int, error) {
	conditions := []string{"1 = 1"}
	args := []any{}

	if filters.HostID > 0 {
		args = append(args, filters.HostID)
		conditions = append(
			conditions,
			fmt.Sprintf("el.host_id = $%d", len(args)),
		)
	}

	if filters.Level != "" {
		args = append(args, filters.Level)
		conditions = append(
			conditions,
			fmt.Sprintf("el.level = $%d", len(args)),
		)
	}

	if filters.AppName != "" {
		args = append(args, filters.AppName)
		conditions = append(
			conditions,
			fmt.Sprintf("el.app_name = $%d", len(args)),
		)
	}

	if filters.Search != "" {
		args = append(args, "%"+filters.Search+"%")
		conditions = append(
			conditions,
			fmt.Sprintf("el.message ILIKE $%d", len(args)),
		)
	}

	whereClause := strings.Join(conditions, " AND ")

	var total int

	countQuery := `
		SELECT COUNT(*)
		FROM error_logs el
		WHERE ` + whereClause

	if err := r.db.QueryRowContext(
		ctx,
		countQuery,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitPosition := len(args) + 1
	offsetPosition := len(args) + 2

	listQuery := fmt.Sprintf(`
		SELECT
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
		WHERE %s
		ORDER BY el.created_at DESC, el.id DESC
		LIMIT $%d
		OFFSET $%d`,
		whereClause,
		limitPosition,
		offsetPosition,
	)

	listArgs := append(
		append([]any{}, args...),
		filters.PageSize,
		(filters.Page-1)*filters.PageSize,
	)

	errorLogs := []models.ErrorLogListItem{}

	if err := r.db.Select(
		ctx,
		&errorLogs,
		listQuery,
		listArgs...,
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
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT DISTINCT app_name
		 FROM error_logs
		 ORDER BY app_name`,
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
