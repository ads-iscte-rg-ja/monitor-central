package repositories

import (
	"context"
	"time"

	"monitor-central/internal/models"
)

func (r *HostsRepository) ListHealthTargets(ctx context.Context, afterID int64, limit int) ([]models.HostHealthTarget, error) {
	targets := []models.HostHealthTarget{}
	err := r.db.Select(ctx, &targets, `SELECT id,health_endpoint FROM hosts WHERE health_endpoint<>'' AND id>$1 ORDER BY id LIMIT $2`, afterID, limit)
	return targets, err
}

func (r *HostsRepository) RecordHealthCheck(ctx context.Context, target models.HostHealthTarget, started time.Time, healthy bool) error {
	// An edited URL or a newer completed probe invalidates an older in-flight result.
	_, err := r.db.ExecContext(ctx, `UPDATE hosts SET health_checked_at=$3,health_check_ok=$4,
 status=CASE WHEN $4 THEN 'online' ELSE 'offline' END,
 unavailable_since=CASE WHEN $4 THEN NULL ELSE COALESCE(unavailable_since,$3) END
 WHERE id=$1 AND health_endpoint=$2 AND (health_checked_at IS NULL OR health_checked_at<=$3)`, target.ID, target.HealthEndpoint, started, healthy)
	return err
}
