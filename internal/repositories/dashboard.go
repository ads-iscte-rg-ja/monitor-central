package repositories

import (
	"context"
	"database/sql"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
)

type DashboardRepository struct {
	db *database.DB
}

func (r *DashboardRepository) GetLatestMetric(ctx context.Context, hostID int64) (*models.MetricPoint, error) {
	var p models.MetricPoint
	err := r.db.Get(ctx, &p, `SELECT cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at FROM machine_stats WHERE host_id=$1 AND collected_at<=CURRENT_TIMESTAMP ORDER BY collected_at DESC,id DESC LIMIT 1`, hostID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func NewDashboardRepository(
	db *database.DB,
) *DashboardRepository {
	return &DashboardRepository{db: db}
}

func (r *DashboardRepository) CountHosts(
	ctx context.Context,
) (int, int, error) {
	var online int
	var total int

	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*) FILTER (WHERE status = 'online'),
			COUNT(*)
		 FROM hosts`,
	).Scan(&online, &total)

	return online, total, err
}

func (r *DashboardRepository) CountErrorsToday(
	ctx context.Context,
) (int, int, error) {
	var total int
	var critical int

	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (
				WHERE level IN ('CRITICAL', 'PANIC')
			)
		 FROM error_logs
		 WHERE created_at >= CURRENT_DATE
		   AND created_at < CURRENT_DATE + INTERVAL '1 day'`,
	).Scan(&total, &critical)

	return total, critical, err
}

func (r *DashboardRepository) CountAlerts(
	ctx context.Context,
) (int, int, error) {
	var active int
	var firing int

	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			(
				SELECT COUNT(*)
				FROM alerts
				WHERE enabled = TRUE
			),
			(
				SELECT COUNT(*)
				FROM alert_events
				WHERE status IN ('fired','acknowledged')
			)`,
	).Scan(&active, &firing)

	return active, firing, err
}

func (r *DashboardRepository) AverageLatestCPU(
	ctx context.Context,
) (float64, error) {
	var average float64

	err := r.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(AVG(cpu_percent), 0)
		 FROM (
			SELECT DISTINCT ON (host_id)
				host_id,
				cpu_percent
			FROM machine_stats
			ORDER BY host_id, collected_at DESC
		 ) AS latest_stats`,
	).Scan(&average)

	return average, err
}

func (r *DashboardRepository) ListDashboardHosts(
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

func (r *DashboardRepository) GetHostMetrics(
	ctx context.Context,
	hostID int64,
	metricsRange string,
) ([]models.MetricPoint, error) {
	var lookback string
	var bucketSize string

	switch metricsRange {
	case "24h":
		lookback = "24 hours"
		bucketSize = "1 hour"

	case "1w":
		lookback = "7 days"
		bucketSize = "6 hours"

	case "1m":
		lookback = "30 days"
		bucketSize = "1 day"

	default:
		return nil, models.ErrInvalidMetricsRange
	}

	stats := []models.MetricPoint{}

	err := r.db.Select(
		ctx,
		&stats,
		`SELECT
			AVG(cpu_percent)::DOUBLE PRECISION
				AS cpu_percent,
			AVG(ram_used_mb)::DOUBLE PRECISION
				AS ram_used_mb,
			MAX(ram_total_mb)::DOUBLE PRECISION
				AS ram_total_mb,
			AVG(disk_used_gb)::DOUBLE PRECISION
				AS disk_used_gb,
			MAX(disk_total_gb)::DOUBLE PRECISION
				AS disk_total_gb,
			date_bin(
				$2::INTERVAL,
				collected_at,
				TIMESTAMPTZ '2000-01-01 00:00:00+00'
			) AS collected_at
		 FROM machine_stats
		 WHERE host_id = $1
		   AND collected_at >= NOW() - $3::INTERVAL
		   AND collected_at <= NOW()
		 GROUP BY date_bin(
			$2::INTERVAL,
			collected_at,
			TIMESTAMPTZ '2000-01-01 00:00:00+00'
		 )
		 ORDER BY collected_at`,
		hostID,
		bucketSize,
		lookback,
	)
	if err != nil {
		return nil, err
	}

	return stats, nil
}
