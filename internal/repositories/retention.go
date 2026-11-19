package repositories

import (
	"context"
	"database/sql"
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
		if counts[category.table], err = cleanupTable(ctx, tx, category.table, category.predicate, category.order, now, category.days, c.BatchSize); err != nil {
			return nil, err
		}
	}
	expired, err := cleanupLoginAttempts(ctx, tx, now, c.BatchSize)
	if err != nil {
		return nil, err
	}
	if expired > 0 {
		counts["login_attempts"] = expired
	}
	return counts, tx.Commit()
}

// cleanupTable deletes at most limit rows of table older than days.
func cleanupTable(ctx context.Context, tx *sql.Tx, table, predicate, order string, now time.Time, days, limit int) (int64, error) {
	if days < 0 || days > 36500 {
		return 0, fmt.Errorf("invalid retention days")
	}
	// All identifiers/predicates are application constants, not user input.
	//nolint:gosec // G201: only the constant table/predicate/order are formatted; values go as $1/$2.
	query := fmt.Sprintf(`WITH candidates AS (SELECT id FROM %s WHERE %s ORDER BY %s LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM %s WHERE id IN (SELECT id FROM candidates)`, table, predicate, order, table)
	result, err := tx.ExecContext(ctx, query, now.AddDate(0, 0, -days), limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup %s: %w", table, err)
	}
	return result.RowsAffected()
}

// cleanupLoginAttempts deletes login limiter windows that have ended (ADR-0007).
func cleanupLoginAttempts(ctx context.Context, tx *sql.Tx, now time.Time, limit int) (int64, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM login_attempts WHERE key IN (SELECT key FROM login_attempts WHERE window_ends < $1 ORDER BY window_ends LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup login_attempts: %w", err)
	}
	return result.RowsAffected()
}
