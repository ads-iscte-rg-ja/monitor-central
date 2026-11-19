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
	for _, category := range retentionCategories {
		days := category.days(c)
		if days == 0 {
			continue
		}
		if counts[category.table], err = cleanupTable(ctx, tx, category, now, days, c.BatchSize); err != nil {
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

// retentionCategory is one table cleaned by retention. Each query is a
// complete constant, never assembled at run time: values go as $1 (cutoff)
// and $2 (batch size).
type retentionCategory struct {
	table string
	query string
	days  func(config.RetentionConfig) int
}

var retentionCategories = []retentionCategory{
	{"machine_stats", `WITH candidates AS (SELECT id FROM machine_stats WHERE collected_at < $1 ORDER BY collected_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM machine_stats WHERE id IN (SELECT id FROM candidates)`, func(c config.RetentionConfig) int { return c.StatsDays }},
	{"error_logs", `WITH candidates AS (SELECT id FROM error_logs WHERE created_at < $1 ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM error_logs WHERE id IN (SELECT id FROM candidates)`, func(c config.RetentionConfig) int { return c.ErrorDays }},
	{"alert_events", `WITH candidates AS (SELECT id FROM alert_events WHERE status='resolved' AND resolved_at < $1
   AND NOT EXISTS (SELECT 1 FROM email_outbox n WHERE n.event_id=alert_events.id AND n.sent_at IS NULL)
   ORDER BY resolved_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM alert_events WHERE id IN (SELECT id FROM candidates)`, func(c config.RetentionConfig) int { return c.ResolvedEventDays }},
	// A two-day minimum for daily-summary rows preserves their daily dedupe keys.
	{"email_outbox", `WITH candidates AS (SELECT id FROM email_outbox WHERE sent_at < $1
   AND (dedupe_key NOT LIKE 'daily:%' OR created_at < CURRENT_TIMESTAMP - INTERVAL '2 days')
   ORDER BY sent_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM email_outbox WHERE id IN (SELECT id FROM candidates)`, func(c config.RetentionConfig) int { return c.SentEmailDays }},
	{"audit_log", `WITH candidates AS (SELECT id FROM audit_log WHERE timestamp < $1 ORDER BY timestamp,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 DELETE FROM audit_log WHERE id IN (SELECT id FROM candidates)`, func(c config.RetentionConfig) int { return c.AuditDays }},
}

// cleanupTable deletes at most limit rows of one category older than days.
func cleanupTable(ctx context.Context, tx *sql.Tx, category retentionCategory, now time.Time, days, limit int) (int64, error) {
	if days < 0 || days > 36500 {
		return 0, fmt.Errorf("invalid retention days")
	}
	result, err := tx.ExecContext(ctx, category.query, now.AddDate(0, 0, -days), limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup %s: %w", category.table, err)
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
