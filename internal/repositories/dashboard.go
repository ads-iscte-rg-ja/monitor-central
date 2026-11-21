package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

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

// GetBufferStatus returns the host's last buffer snapshot (ADR-0010), or nil
// if its agent never reported one.
func (r *DashboardRepository) GetBufferStatus(ctx context.Context, hostID int64) (*models.BufferStatus, error) {
	var status models.BufferStatus
	var dropped []byte
	err := r.db.QueryRowContext(ctx, `SELECT observed_at,pending,quarantined,oldest_pending_age_seconds,
 delivery_paused,size_bytes,dropped_by_kind,received_at FROM host_buffer_status WHERE host_id=$1`, hostID).Scan(
		&status.ObservedAt, &status.Pending, &status.Quarantined, &status.OldestPendingAgeSeconds,
		&status.DeliveryPaused, &status.SizeBytes, &dropped, &status.ReceivedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(dropped, &status.DroppedByKind); err != nil {
		return nil, fmt.Errorf("decode buffer drops: %w", err)
	}
	return &status, nil
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

	// From the hourly counters (ADR-0017), not a COUNT(*) over the day's
	// errors: at most 24 rows per host and level. The buckets are UTC hours,
	// so the day boundary is exact for any whole-hour offset of CURRENT_DATE.
	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(count), 0),
			COALESCE(SUM(count) FILTER (
				WHERE level IN ('CRITICAL', 'PANIC')
			), 0)
		 FROM error_counts_hourly
		 WHERE bucket >= CURRENT_DATE
		   AND bucket < CURRENT_DATE + INTERVAL '1 day'`,
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

	// One index lookup per host for its latest sample (ADR-0017), instead of
	// sorting every sample with DISTINCT ON. Hosts without samples are left
	// out, as before.
	err := r.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(AVG(latest.cpu_percent), 0)
		 FROM hosts h
		 CROSS JOIN LATERAL (
			SELECT cpu_percent
			FROM machine_stats
			WHERE host_id = h.id
			ORDER BY collected_at DESC
			LIMIT 1
		 ) AS latest`,
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
