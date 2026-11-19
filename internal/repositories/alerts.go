package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"monitor-central/internal/alertmetric"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/models"

	"github.com/lib/pq"
)

type AlertsRepository struct {
	db *database.DB
}

func NewAlertsRepository(
	db *database.DB,
) *AlertsRepository {
	return &AlertsRepository{db: db}
}

func (r *AlertsRepository) ListAlerts(
	ctx context.Context,
) ([]models.AlertRuleListItem, error) {
	alerts := []models.AlertRuleListItem{}

	err := r.db.Select(
		ctx,
		&alerts,
		`SELECT
			a.id,
			a.name,
			a.host_id,
			COALESCE(h.name, 'All hosts') AS host_name,
			a.metric,
			a.condition,
			a.threshold::DOUBLE PRECISION AS threshold,
			a.severity,
			array_to_string(
				a.notification_channels,
				','
			) AS channels_csv,
			a.enabled
		 FROM alerts a
		 LEFT JOIN hosts h ON h.id = a.host_id
		 ORDER BY a.created_at DESC, a.id`,
	)
	if err != nil {
		return nil, err
	}

	return alerts, nil
}

func (r *AlertsRepository) ListAlertEvents(
	ctx context.Context,
	limit int,
) ([]models.AlertEventListItem, error) {
	events := []models.AlertEventListItem{}

	err := r.db.Select(
		ctx,
		&events,
		`SELECT
			ae.id,
			a.name AS alert_name,
			ae.host_id,
			h.name AS host_name,
			a.metric,
			ae.triggered_value::DOUBLE PRECISION
				AS triggered_value,
			ae.status,
			ae.fired_at
		 FROM alert_events ae
		 JOIN alerts a ON a.id = ae.alert_id
		 JOIN hosts h ON h.id = ae.host_id
		 ORDER BY ae.fired_at DESC, ae.id
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func (r *AlertsRepository) ListAlertHosts(
	ctx context.Context,
) ([]models.AlertHostOption, error) {
	hosts := []models.AlertHostOption{}

	err := r.db.Select(
		ctx,
		&hosts,
		`SELECT
			id,
			name,
			hostname
		 FROM hosts
		 ORDER BY name, id`,
	)
	if err != nil {
		return nil, err
	}

	return hosts, nil
}

func (r *AlertsRepository) CreateAlert(
	ctx context.Context,
	alert *models.AlertRule,
) error {
	err := r.db.QueryRowContext(
		ctx,
		`INSERT INTO alerts (
			name,
			host_id,
			metric,
			condition,
			threshold,
			severity,
			notification_channels,
			enabled
		 )
		 VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8
		 ) RETURNING id`,
		alert.Name,
		alert.HostID,
		alert.Metric,
		alert.Condition,
		alert.Threshold,
		alert.Severity,
		pq.Array(alert.Channels),
		alert.Enabled,
	).Scan(&alert.ID)

	return err
}

func (r *AlertsRepository) DeleteAlert(
	ctx context.Context,
	alertID int64,
) error {
	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM alerts
		 WHERE id = $1`,
		alertID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrAlertNotFound
	}

	return nil
}

func (r *AlertsRepository) UpdateAlert(
	ctx context.Context,
	alert *models.AlertRule,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Editing a rule closes incidents evaluated against its previous definition.
	if _, err = tx.ExecContext(ctx, `SELECT id FROM alerts WHERE id=$1 FOR UPDATE`, alert.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE alerts
		 SET name = $1,
		     host_id = $2,
		     metric = $3,
		     condition = $4,
		     threshold = $5,
		     severity = $6,
		     notification_channels = $7,
		     enabled = $8,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $9`,
		alert.Name,
		alert.HostID,
		alert.Metric,
		alert.Condition,
		alert.Threshold,
		alert.Severity,
		pq.Array(alert.Channels),
		alert.Enabled,
		alert.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrAlertNotFound
	}

	if _, err = tx.ExecContext(ctx, `UPDATE alert_events SET status='resolved',resolved_at=CURRENT_TIMESTAMP WHERE alert_id=$1 AND status IN ('fired','acknowledged')`, alert.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMetricEvaluations returns every enabled rule with its metric's current
// value: one set-based query per metric, each provided by the metric itself
// (ADR-0008), ordered by rule.
func (r *AlertsRepository) ListMetricEvaluations(ctx context.Context) ([]models.AlertMetricEvaluation, error) {
	evaluations := []models.AlertMetricEvaluation{}
	for _, metric := range alertmetric.All() {
		var rules []models.AlertMetricEvaluation
		query, args := metric.Query()
		if err := r.db.Select(ctx, &rules, query, args...); err != nil {
			return nil, fmt.Errorf("evaluate %s alerts: %w", metric.Name(), err)
		}
		evaluations = append(evaluations, rules...)
	}
	sort.Slice(evaluations, func(i, j int) bool { return evaluations[i].AlertID < evaluations[j].AlertID })
	return evaluations, nil
}

func (r *AlertsRepository) HasOpenAlertEvent(
	ctx context.Context,
	alertID int64,
	hostID int64,
) (bool, error) {
	var exists bool

	err := r.db.Get(
		ctx,
		&exists,
		`SELECT EXISTS (
			SELECT 1
			FROM alert_events
			WHERE alert_id = $1
			  AND host_id = $2
			  AND status IN (
				'fired',
				'acknowledged'
			  )
		 )`,
		alertID,
		hostID,
	)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// The unique open-event index serializes concurrent evaluators. Outbox rows and
// event creation either all commit or all roll back; SMTP never runs here.
func (r *AlertsRepository) CreateAlertEventWithNotifications(ctx context.Context, event *models.AlertEvent, notifications []models.EmailNotification) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Lock the current rule so a concurrent edit cannot orphan a newly fired event.
	var enabled bool
	var hostID sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT enabled,host_id FROM alerts WHERE id=$1 FOR SHARE", event.AlertID).Scan(&enabled, &hostID); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !enabled || !hostID.Valid || hostID.Int64 != event.HostID {
		return false, nil
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO alert_events(alert_id,host_id,triggered_value,status,fired_at) VALUES($1,$2,$3,'fired',$4)
 ON CONFLICT(alert_id,host_id) WHERE status IN ('fired','acknowledged') DO NOTHING RETURNING id`, event.AlertID, event.HostID, event.TriggeredValue, event.FiredAt).Scan(&event.ID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, n := range notifications {
		n.EventID = &event.ID
		n.DedupeKey = fmt.Sprintf("event:%d:%s", event.ID, n.Recipient)
		if err := insertNotification(ctx, tx, n); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// ResolveOpenAlertEvents resolves the open events of every (alert, host) pair
// with one statement (ADR-0005).
func (r *AlertsRepository) ResolveOpenAlertEvents(ctx context.Context, targets []models.AlertTarget) error {
	if len(targets) == 0 {
		return nil
	}
	alerts := make([]int64, len(targets))
	hosts := make([]int64, len(targets))
	for i, t := range targets {
		alerts[i], hosts[i] = t.AlertID, t.HostID
	}
	_, err := r.db.ExecContext(ctx, `UPDATE alert_events e
		 SET status = 'resolved', resolved_at = CURRENT_TIMESTAMP
		 FROM unnest($1::bigint[], $2::bigint[]) AS t(alert_id, host_id)
		 WHERE e.alert_id = t.alert_id AND e.host_id = t.host_id
		   AND e.status IN ('fired', 'acknowledged')`,
		pq.Array(alerts), pq.Array(hosts))
	return err
}

func (r *AlertsRepository) ListNotificationEmails(
	ctx context.Context,
) ([]string, error) {
	type emailRow struct {
		Email string `db:"email"`
	}

	rows := []emailRow{}

	err := r.db.Select(
		ctx,
		&rows,
		`SELECT DISTINCT notification_email AS email
		FROM users
		WHERE notification_email <> ''
		ORDER BY notification_email`,
	)
	if err != nil {
		return nil, err
	}

	emails := make([]string, 0, len(rows))

	for _, row := range rows {
		emails = append(emails, row.Email)
	}

	return emails, nil
}

func (r *AlertsRepository) AcknowledgeAlertEvent(
	ctx context.Context,
	eventID int64,
) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE alert_events
		 SET status = 'acknowledged'
		 WHERE id = $1
		   AND status = 'fired'`,
		eventID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrAlertEventNotFound
	}

	return nil
}

func (r *AlertsRepository) EnsureOfflineAlertRules(
	ctx context.Context,
	thresholdMinutes float64,
) error {
	// Passive system rules follow each host's contact timeout. Endpoint-backed
	// rules retain their separately editable continuous-failure threshold.
	if _, err := r.db.ExecContext(ctx, `UPDATE alerts a SET threshold=h.contact_timeout_seconds::DOUBLE PRECISION/60
 FROM hosts h WHERE a.host_id=h.id AND a.system_rule=TRUE AND a.metric='host_offline'
 AND h.health_endpoint='' AND a.threshold<>h.contact_timeout_seconds::DOUBLE PRECISION/60`); err != nil {
		return err
	}
	_, err := r.db.Insert(
		ctx,
		`INSERT INTO alerts (
			name,
			host_id,
			metric,
			condition,
			threshold,
			severity,
			notification_channels,
			enabled,
			system_rule
		)
		SELECT
			'Check ' || h.name || ' — host offline',
			h.id,
			'host_offline',
			'gt',
			CASE WHEN h.health_endpoint='' THEN h.contact_timeout_seconds::DOUBLE PRECISION/60 ELSE $1 END,
			'critical',
			ARRAY['dashboard'],
			TRUE,
			TRUE
		FROM hosts h
		WHERE NOT EXISTS (
			SELECT 1
			FROM alerts a
			WHERE a.host_id = h.id
			  AND a.metric = 'host_offline'
		)
		ON CONFLICT (host_id) WHERE system_rule DO NOTHING`,
		thresholdMinutes,
	)

	return err
}

func (r *AlertsRepository) MarkHostsOffline(
	ctx context.Context,
) error {
	_, err := r.db.Update(
		ctx,
		`UPDATE hosts SET status=CASE
 WHEN health_endpoint<>'' THEN CASE
   WHEN health_checked_at IS NULL OR health_checked_at<CURRENT_TIMESTAMP-($1*INTERVAL '1 second') THEN 'unknown'
   WHEN health_check_ok THEN 'online' ELSE 'offline' END
 WHEN last_seen_at IS NULL THEN 'unknown'
 WHEN last_seen_at<CURRENT_TIMESTAMP-(contact_timeout_seconds*INTERVAL '1 second') THEN 'offline' ELSE 'online' END,
 unavailable_since=CASE WHEN health_endpoint<>'' THEN unavailable_since
 WHEN last_seen_at<CURRENT_TIMESTAMP-(contact_timeout_seconds*INTERVAL '1 second') THEN COALESCE(unavailable_since,last_seen_at)
 ELSE NULL END`,
		config.HostHealthFreshness.Seconds(),
	)

	return err
}

func (r *AlertsRepository) ListAlertEventsSince(
	ctx context.Context,
	since time.Time,
) ([]models.DailyAlertSummaryItem, error) {
	events := []models.DailyAlertSummaryItem{}

	err := r.db.Select(
		ctx,
		&events,
		`SELECT
			a.name AS alert_name,
			h.name AS host_name,
			a.metric,
			a.severity,
			ae.triggered_value::DOUBLE PRECISION
				AS triggered_value,
			ae.status,
			ae.fired_at,
			ae.resolved_at
		 FROM alert_events ae
		 JOIN alerts a ON a.id = ae.alert_id
		 JOIN hosts h ON h.id = ae.host_id
		 WHERE ae.fired_at >= $1
		 ORDER BY ae.fired_at DESC, ae.id`,
		since,
	)
	if err != nil {
		return nil, err
	}

	return events, nil
}
