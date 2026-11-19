package repositories

import (
	"context"
	"database/sql"
	"monitor-central/internal/models"
	"time"
)

func insertNotification(ctx context.Context, tx *sql.Tx, n models.EmailNotification) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO email_outbox(event_id,recipient,subject,text_body,html_body,dedupe_key) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(dedupe_key) DO NOTHING`, n.EventID, n.Recipient, n.Subject, n.TextBody, n.HTMLBody, n.DedupeKey)
	return err
}
func (r *AlertsRepository) QueueNotifications(ctx context.Context, notifications []models.EmailNotification) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range notifications {
		if err := insertNotification(ctx, tx, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *AlertsRepository) ClaimNotification(ctx context.Context) (*models.EmailNotification, error) {
	var n models.EmailNotification
	err := r.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT id FROM email_outbox WHERE sent_at IS NULL AND next_attempt_at<=CURRENT_TIMESTAMP
 AND (locked_until IS NULL OR locked_until<CURRENT_TIMESTAMP) ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE email_outbox n SET locked_until=CURRENT_TIMESTAMP+INTERVAL '2 minutes',attempts=n.attempts+1
 FROM candidate c WHERE n.id=c.id RETURNING n.id,n.event_id,n.recipient,n.subject,n.text_body,n.html_body,n.dedupe_key,n.attempts`).Scan(&n.ID, &n.EventID, &n.Recipient, &n.Subject, &n.TextBody, &n.HTMLBody, &n.DedupeKey, &n.Attempts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}
func (r *AlertsRepository) CompleteNotification(ctx context.Context, id int64, attempt int, sendErr error) error {
	if sendErr == nil {
		_, err := r.db.ExecContext(ctx, `UPDATE email_outbox SET sent_at=CURRENT_TIMESTAMP,locked_until=NULL,last_error=NULL WHERE id=$1 AND attempts=$2 AND sent_at IS NULL`, id, attempt)
		return err
	}
	exponent := min(attempt-1, 8)
	delay := min(15*time.Second*time.Duration(1<<exponent), time.Hour)
	// Avoid persisting SMTP errors containing recipient data or server-provided secrets.
	_, err := r.db.ExecContext(ctx, `UPDATE email_outbox SET locked_until=NULL,last_error='SMTP delivery failed',next_attempt_at=CURRENT_TIMESTAMP+($3*INTERVAL '1 second') WHERE id=$1 AND attempts=$2 AND sent_at IS NULL`, id, attempt, delay.Seconds())
	return err
}

// CountPendingNotifications returns how many notifications are still unsent
// (the outbox backlog, exposed as a metric).
func (r *AlertsRepository) CountPendingNotifications(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_outbox WHERE sent_at IS NULL`).Scan(&n)
	return n, err
}
