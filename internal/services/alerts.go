package services

import (
	"context"
	"errors"
	"fmt"
	"html"
	"monitor-central/internal/alertmetric"
	"monitor-central/internal/models"
	"strings"
	"time"
)

type AlertsRepository interface {
	ListAlerts(context.Context) ([]models.AlertRuleListItem, error)
	ListAlertEvents(context.Context, int) ([]models.AlertEventListItem, error)
	ListAlertHosts(context.Context) ([]models.AlertHostOption, error)
	CreateAlert(context.Context, *models.AlertRule) error
	DeleteAlert(context.Context, int64) error
	UpdateAlert(context.Context, *models.AlertRule) error
	ListMetricEvaluations(context.Context) ([]models.AlertMetricEvaluation, error)
	CreateAlertEventWithNotifications(context.Context, *models.AlertEvent, []models.EmailNotification) (bool, error)
	ResolveOpenAlertEvents(context.Context, []models.AlertTarget) error
	ListNotificationEmails(context.Context) ([]string, error)
	AcknowledgeAlertEvent(context.Context, int64) error
	EnsureOfflineAlertRules(context.Context, float64) error
	MarkHostsOffline(context.Context) error
	ListAlertEventsSince(context.Context, time.Time) ([]models.DailyAlertSummaryItem, error)
	QueueNotifications(context.Context, []models.EmailNotification) error
	ClaimNotification(context.Context) (*models.EmailNotification, error)
	CompleteNotification(context.Context, int64, int, error) error
}
type AlertEmailSender interface {
	SendNotification(context.Context, models.EmailNotification) error
}
type AlertsService struct {
	repo        AlertsRepository
	emailSender AlertEmailSender
}

func NewAlertsService(repo AlertsRepository, senders ...AlertEmailSender) *AlertsService {
	s := &AlertsService{repo: repo}
	if len(senders) > 0 {
		s.emailSender = senders[0]
	}
	return s
}

func (s *AlertsService) GetPage(ctx context.Context) (*models.AlertsPage, error) {
	alerts, err := s.repo.ListAlerts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range alerts {
		alerts[i].Channels = splitChannels(alerts[i].ChannelsCSV)
		alerts[i].ThresholdDisplay = formatAlertThreshold(alerts[i])
	}
	events, err := s.repo.ListAlertEvents(ctx, 20)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].FiredAtDisplay = events[i].FiredAt.In(loc).Format("02 Jan 2006, 15:04")
		events[i].TriggeredValueDisplay = formatAlertValue(events[i].Metric, events[i].TriggeredValue)
	}
	hosts, err := s.repo.ListAlertHosts(ctx)
	if err != nil {
		return nil, err
	}
	return &models.AlertsPage{Alerts: alerts, AlertEvents: events, Hosts: hosts}, nil
}

func validateAlert(name string, hostID int64, metric, condition string, threshold float64, severity string, channels []string, enabled bool) (*models.AlertRule, error) {
	name = strings.TrimSpace(name)
	metric = strings.ToLower(strings.TrimSpace(metric))
	condition = strings.ToLower(strings.TrimSpace(condition))
	severity = strings.ToLower(strings.TrimSpace(severity))
	if name == "" {
		return nil, models.ErrAlertNameRequired
	}
	if len(name) > 200 {
		return nil, models.ErrFieldTooLarge
	}
	if hostID <= 0 {
		return nil, models.ErrAlertHostRequired
	}
	m, known := alertmetric.Lookup(metric)
	if !known {
		return nil, models.ErrInvalidAlertMetric
	}
	if condition != "gt" && condition != "lt" {
		return nil, models.ErrInvalidAlertCondition
	}
	if !finite(threshold) || threshold <= 0 || !m.ValidThreshold(threshold) {
		return nil, models.ErrInvalidAlertThreshold
	}
	if severity != "warning" && severity != "critical" {
		return nil, models.ErrInvalidAlertSeverity
	}
	clean, err := cleanChannels(channels)
	if err != nil {
		return nil, err
	}
	return &models.AlertRule{Name: name, HostID: hostID, Metric: metric, Condition: condition, Threshold: threshold, Severity: severity, Channels: clean, Enabled: enabled}, nil
}
func (s *AlertsService) CreateAlert(ctx context.Context, r models.CreateAlertRequest) (*models.AlertRule, error) {
	a, err := validateAlert(r.Name, r.HostID, r.Metric, r.Condition, r.Threshold, r.Severity, r.Channels, r.Enabled)
	if err != nil {
		return nil, err
	}
	if err = s.repo.CreateAlert(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *AlertsService) UpdateAlert(ctx context.Context, id int64, r models.UpdateAlertRequest) error {
	if id <= 0 {
		return models.ErrAlertIDRequired
	}
	a, err := validateAlert(r.Name, r.HostID, r.Metric, r.Condition, r.Threshold, r.Severity, r.Channels, r.Enabled)
	if err != nil {
		return err
	}
	a.ID = id
	return s.repo.UpdateAlert(ctx, a)
}
func (s *AlertsService) DeleteAlert(ctx context.Context, id int64) error {
	if id <= 0 {
		return models.ErrAlertIDRequired
	}
	return s.repo.DeleteAlert(ctx, id)
}
func (s *AlertsService) AcknowledgeAlertEvent(ctx context.Context, id int64) error {
	if id <= 0 {
		return models.ErrAlertEventIDRequired
	}
	return s.repo.AcknowledgeAlertEvent(ctx, id)
}
func (s *AlertsService) EvaluateHostAvailability(ctx context.Context) error {
	if err := s.repo.EnsureOfflineAlertRules(ctx, 5); err != nil {
		return err
	}
	return s.repo.MarkHostsOffline(ctx)
}
func (s *AlertsService) EvaluateMetricAlerts(ctx context.Context) error {
	evaluations, err := s.repo.ListMetricEvaluations(ctx)
	if err != nil {
		return err
	}
	// Rules no longer breached are resolved together, in one statement per
	// cycle, instead of one UPDATE per rule (ADR-0005). They are resolved even
	// if firing another alert failed, as they were before.
	var cleared []models.AlertTarget
	fireErr := s.fireBreachedAlerts(ctx, evaluations, &cleared)
	if len(cleared) > 0 {
		if err := s.repo.ResolveOpenAlertEvents(ctx, cleared); err != nil {
			return errors.Join(fireErr, fmt.Errorf("resolve alert events: %w", err))
		}
	}
	return fireErr
}

func (s *AlertsService) fireBreachedAlerts(ctx context.Context, evaluations []models.AlertMetricEvaluation, cleared *[]models.AlertTarget) error {
	recipients := &lazyRecipients{repo: s.repo}
	for _, e := range evaluations {
		if !breached(e) {
			*cleared = append(*cleared, models.AlertTarget{AlertID: e.AlertID, HostID: e.HostID})
			continue
		}
		notifications, err := recipients.notifications(ctx, e)
		if err != nil {
			return err
		}
		event := &models.AlertEvent{AlertID: e.AlertID, HostID: e.HostID, TriggeredValue: e.CurrentValue, Status: "fired", FiredAt: time.Now().UTC()}
		if _, err := s.repo.CreateAlertEventWithNotifications(ctx, event, notifications); err != nil {
			return fmt.Errorf("commit alert and outbox: %w", err)
		}
	}
	return nil
}

// SMTP delivery is independent of incident evaluation. A crash after SMTP
// acceptance but before completion can cause a retry: delivery is at-least-once.
func (s *AlertsService) DeliverNotifications(ctx context.Context) error {
	if s.emailSender == nil {
		return nil
	}
	var failures []error
	for i := 0; i < 50; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := s.repo.ClaimNotification(ctx)
		if err != nil {
			return err
		}
		if n == nil {
			break
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		sendErr := s.emailSender.SendNotification(sendCtx, *n)
		cancel()
		if err := s.repo.CompleteNotification(ctx, n.ID, n.Attempts, sendErr); err != nil {
			return err
		}
		if sendErr != nil {
			failures = append(failures, fmt.Errorf("notification %d delivery failed: %w", n.ID, sendErr))
		}
	}
	return errors.Join(failures...)
}

func metricNotification(e models.AlertMetricEvaluation, recipient string) models.EmailNotification {
	subject := fmt.Sprintf("[%s] %s on %s", strings.ToUpper(e.Severity), e.AlertName, e.HostName)
	value := formatAlertValue(e.Metric, e.CurrentValue)
	body := fmt.Sprintf("Alert: %s\nHost: %s\nMetric: %s\nValue: %s\nThreshold: %g\nSeverity: %s", e.AlertName, e.HostName, e.Metric, value, e.Threshold, e.Severity)
	return models.EmailNotification{Recipient: recipient, Subject: subject, TextBody: body, HTMLBody: "<h1>" + html.EscapeString(e.AlertName) + "</h1><pre>" + html.EscapeString(body) + "</pre>"}
}
func (s *AlertsService) SendDailyAlertSummary(ctx context.Context, now time.Time) error {
	events, err := s.repo.ListAlertEventsSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	recipients, err := s.repo.ListNotificationEmails(ctx)
	if err != nil {
		return err
	}
	var body strings.Builder
	body.WriteString("Alert events from the last 24 hours\n\n")
	for _, e := range events {
		fmt.Fprintf(&body, "%s | %s | %s | %s | %s\n", e.AlertName, e.HostName, formatAlertValue(e.Metric, e.TriggeredValue), e.Status, e.FiredAt.Format(time.RFC3339))
	}
	if len(events) == 0 {
		body.WriteString("No alert events recorded.")
	}
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return err
	}
	date := now.In(loc).Format("2006-01-02")
	notifications := make([]models.EmailNotification, 0, len(recipients))
	for _, recipient := range recipients {
		notifications = append(notifications, models.EmailNotification{Recipient: recipient, Subject: "[DAILY SUMMARY] Alerts — " + date, TextBody: body.String(), HTMLBody: "<h1>Daily alert summary</h1><pre>" + html.EscapeString(body.String()) + "</pre>", DedupeKey: "daily:" + date + ":" + recipient})
	}
	return s.repo.QueueNotifications(ctx, notifications)
}
func splitChannels(value string) []string {
	result := []string{}
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	return result
}
func hasNotificationChannel(value, target string) bool {
	for _, c := range splitChannels(value) {
		if c == target {
			return true
		}
	}
	return false
}

// cleanChannels normalises the notification channels: known values only, no
// repeats, at least one.
func cleanChannels(channels []string) ([]string, error) {
	clean := []string{}
	seen := map[string]bool{}
	for _, value := range channels {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "email" && value != "dashboard" {
			return nil, models.ErrAlertChannelRequired
		}
		if !seen[value] {
			clean = append(clean, value)
			seen[value] = true
		}
	}
	if len(clean) == 0 {
		return nil, models.ErrAlertChannelRequired
	}
	return clean, nil
}

// breached asks the rule's metric (ADR-0008). A rule of a metric no longer
// registered never fires.
func breached(e models.AlertMetricEvaluation) bool {
	m, ok := alertmetric.Lookup(e.Metric)
	return ok && m.Breached(e.Condition, e.CurrentValue, e.Threshold)
}

func formatAlertValue(metric string, value float64) string {
	if m, ok := alertmetric.Lookup(metric); ok {
		return m.Format(value)
	}
	return fmt.Sprintf("%g", value)
}

func formatAlertThreshold(a models.AlertRuleListItem) string {
	if m, ok := alertmetric.Lookup(a.Metric); ok {
		return m.FormatThreshold(a.Condition, a.Threshold)
	}
	return fmt.Sprintf("%s %g", a.Condition, a.Threshold)
}

// lazyRecipients loads the notification emails once per evaluation cycle, and
// only if some breached rule notifies by email.
type lazyRecipients struct {
	repo   AlertsRepository
	emails []string
	loaded bool
}

func (l *lazyRecipients) notifications(ctx context.Context, e models.AlertMetricEvaluation) ([]models.EmailNotification, error) {
	if !hasNotificationChannel(e.ChannelsCSV, "email") {
		return nil, nil
	}
	if !l.loaded {
		emails, err := l.repo.ListNotificationEmails(ctx)
		if err != nil {
			return nil, err
		}
		l.emails, l.loaded = emails, true
	}
	notifications := make([]models.EmailNotification, 0, len(l.emails))
	for _, recipient := range l.emails {
		notifications = append(notifications, metricNotification(e, recipient))
	}
	return notifications, nil
}
