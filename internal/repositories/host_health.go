package repositories

import (
	"context"
	"time"

	"monitor-central/internal/models"
)

// ClaimDueHealthTargets returns up to limit hosts whose probe is due at now and,
// in the same statement, books their next one at now + interval ± 10 %
// (ADR-0013). The jitter spreads the probes over the interval; booking at claim
// time means a late iteration never probes the same host twice.
func (r *HostsRepository) ClaimDueHealthTargets(ctx context.Context, now time.Time, interval time.Duration, limit int) ([]models.HostHealthTarget, error) {
	targets := []models.HostHealthTarget{}
	err := r.db.Select(ctx, &targets, `WITH due AS (
 SELECT id FROM hosts WHERE health_endpoint<>'' AND (next_check_at IS NULL OR next_check_at<=$1)
 ORDER BY next_check_at NULLS FIRST, id LIMIT $3 FOR UPDATE SKIP LOCKED)
UPDATE hosts h SET next_check_at=$1 + $2 * (0.9 + 0.2 * random()) * INTERVAL '1 second'
FROM due WHERE h.id=due.id
RETURNING h.id, h.health_endpoint`, now, interval.Seconds(), limit)
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
