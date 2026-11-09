package repositories

import (
	"context"
	"fmt"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/database"
)

type RetentionRepository struct{ db *database.DB }

func NewRetentionRepository(db *database.DB) *RetentionRepository {
	return &RetentionRepository{db: db}
}

// Cleanup removes at most BatchSize rows per category per run. Pending email,
// open incidents, and telemetry receipts are never retention candidates.
func (r *RetentionRepository) Cleanup(ctx context.Context, c config.RetentionConfig, now time.Time) (map[string]int64, error) {
	if c.BatchSize < 1 || c.BatchSize > 10000 {
		return nil, fmt.Errorf("invalid cleanup batch size")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(743819114)").Scan(&locked); err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	if !locked {
		return counts, nil
	}
	for _, category := range []struct {
		table, predicate, order string
		days                    int
	}{
		{"machine_stats", "collected_at < $1", "collected_at,id", c.StatsDays},
		{"error_logs", "created_at < $1", "created_at,id", c.ErrorDays},
		{"alert_events", "status='resolved' AND resolved_at < $1 AND NOT EXISTS (SELECT 1 FROM email_outbox n WHERE n.event_id=alert_events.id AND n.sent_at IS NULL)", "resolved_at,id", c.ResolvedEventDays},
		// A two-day minimum for daily-summary rows preserves their daily dedupe keys.
		{"email_outbox", "sent_at < $1 AND (dedupe_key NOT LIKE 'daily:%' OR created_at < CURRENT_TIMESTAMP - INTERVAL '2 days')", "sent_at,id", c.SentEmailDays},
		{"audit_log", "timestamp < $1", "timestamp,id", c.AuditDays},
	} {
		if category.days == 0 {
			continue
		}
		if category.days < 0 || category.days > 36500 {
			return nil, fmt.Errorf("invalid retention days")
		}
		// All identifiers/predicates above are application constants, not user input.
		query := fmt.Sprintf(`WITH candidates AS (SELECT id FROM %s WHERE %s ORDER BY %s LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM %s WHERE id IN (SELECT id FROM candidates)`, category.table, category.predicate, category.order, category.table)
		result, err := tx.ExecContext(ctx, query, now.AddDate(0, 0, -category.days), c.BatchSize)
		if err != nil {
			return nil, fmt.Errorf("cleanup %s: %w", category.table, err)
		}
		counts[category.table], err = result.RowsAffected()
		if err != nil {
			return nil, err
		}
	}
	return counts, tx.Commit()
}
