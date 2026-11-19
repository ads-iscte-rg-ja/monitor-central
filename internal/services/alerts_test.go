package services

import (
	"context"
	"errors"
	"math"
	"monitor-central/internal/models"
	"testing"
	"time"
)

type fakeAlertsRepository struct {
	evaluations   []models.AlertMetricEvaluation
	events        []models.AlertEvent
	notifications []models.EmailNotification
	pending       []*models.EmailNotification
	completed     map[int64]error
	resolved      bool
}

func (f *fakeAlertsRepository) ListAlerts(context.Context) ([]models.AlertRuleListItem, error) {
	return []models.AlertRuleListItem{{ID: 1, Metric: "cpu", Condition: "gt", Threshold: 80, ChannelsCSV: "email,dashboard"}}, nil
}
func (f *fakeAlertsRepository) ListAlertEvents(context.Context, int) ([]models.AlertEventListItem, error) {
	return []models.AlertEventListItem{{Metric: "cpu", TriggeredValue: 90, FiredAt: time.Date(2026, 8, 13, 16, 30, 0, 0, time.UTC)}}, nil
}
func (f *fakeAlertsRepository) ListAlertHosts(context.Context) ([]models.AlertHostOption, error) {
	return []models.AlertHostOption{{ID: 1}}, nil
}
func (f *fakeAlertsRepository) CreateAlert(_ context.Context, a *models.AlertRule) error {
	a.ID = 1
	return nil
}
func (f *fakeAlertsRepository) DeleteAlert(context.Context, int64) error             { return nil }
func (f *fakeAlertsRepository) UpdateAlert(context.Context, *models.AlertRule) error { return nil }
func (f *fakeAlertsRepository) ListMetricEvaluations(context.Context) ([]models.AlertMetricEvaluation, error) {
	return f.evaluations, nil
}
func (f *fakeAlertsRepository) CreateAlertEventWithNotifications(_ context.Context, e *models.AlertEvent, n []models.EmailNotification) (bool, error) {
	if len(f.events) > 0 {
		return false, nil
	}
	e.ID = 1
	f.events = append(f.events, *e)
	f.notifications = append(f.notifications, n...)
	return true, nil
}
func (f *fakeAlertsRepository) ResolveOpenAlertEvents(_ context.Context, targets []models.AlertTarget) error {
	f.resolved = len(targets) > 0
	return nil
}
func (f *fakeAlertsRepository) ListNotificationEmails(context.Context) ([]string, error) {
	return []string{"first@example.com", "second@example.com"}, nil
}
func (f *fakeAlertsRepository) AcknowledgeAlertEvent(context.Context, int64) error     { return nil }
func (f *fakeAlertsRepository) EnsureOfflineAlertRules(context.Context, float64) error { return nil }
func (f *fakeAlertsRepository) MarkHostsOffline(context.Context) error                 { return nil }
func (f *fakeAlertsRepository) ListAlertEventsSince(context.Context, time.Time) ([]models.DailyAlertSummaryItem, error) {
	return nil, nil
}
func (f *fakeAlertsRepository) QueueNotifications(_ context.Context, n []models.EmailNotification) error {
	f.notifications = append(f.notifications, n...)
	return nil
}
func (f *fakeAlertsRepository) ClaimNotification(context.Context) (*models.EmailNotification, error) {
	if len(f.pending) == 0 {
		return nil, nil
	}
	n := f.pending[0]
	f.pending = f.pending[1:]
	return n, nil
}
func (f *fakeAlertsRepository) CompleteNotification(_ context.Context, id int64, _ int, err error) error {
	if f.completed == nil {
		f.completed = map[int64]error{}
	}
	f.completed[id] = err
	return nil
}

type fakeEmailSender struct{ calls int }

func (f *fakeEmailSender) SendNotification(_ context.Context, n models.EmailNotification) error {
	f.calls++
	if n.ID == 1 {
		return errors.New("temporary SMTP failure")
	}
	return nil
}
func TestAlertValidationAndPage(t *testing.T) {
	repo := &fakeAlertsRepository{}
	s := NewAlertsService(repo)
	for _, threshold := range []float64{math.NaN(), math.Inf(1), 0, 101} {
		_, err := s.CreateAlert(context.Background(), models.CreateAlertRequest{Name: "CPU", HostID: 1, Metric: "cpu", Condition: "gt", Threshold: threshold, Severity: "critical", Channels: []string{"email"}})
		if !errors.Is(err, models.ErrInvalidAlertThreshold) {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateAlert(context.Background(), models.CreateAlertRequest{Name: "CPU", HostID: 1, Metric: "cpu", Condition: "gt", Threshold: 80, Severity: "critical", Channels: []string{"discord"}}); !errors.Is(err, models.ErrAlertChannelRequired) {
		t.Fatal(err)
	}
	a, err := s.CreateAlert(context.Background(), models.CreateAlertRequest{Name: " CPU ", HostID: 1, Metric: "cpu", Condition: "gt", Threshold: 80, Severity: "critical", Channels: []string{"email", "email"}})
	if err != nil || a.ID != 1 || a.Name != "CPU" || len(a.Channels) != 1 {
		t.Fatalf("unexpected alert %+v %v", a, err)
	}
	page, err := s.GetPage(context.Background())
	if err != nil || page.Alerts[0].ThresholdDisplay != "> 80.0%" || page.AlertEvents[0].FiredAtDisplay != "13 Aug 2026, 17:30" {
		t.Fatalf("unexpected page %+v %v", page, err)
	}
}
func TestAlertEvaluationQueuesEmailWithoutSMTP(t *testing.T) {
	repo := &fakeAlertsRepository{evaluations: []models.AlertMetricEvaluation{{AlertID: 1, HostID: 2, AlertName: "CPU <unsafe>", HostName: "server", Metric: "cpu", Condition: "gt", Threshold: 80, CurrentValue: 95, ChannelsCSV: "email", Severity: "critical"}}}
	sender := &fakeEmailSender{}
	s := NewAlertsService(repo, sender)
	for i := 0; i < 2; i++ {
		if err := s.EvaluateMetricAlerts(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if sender.calls != 0 || len(repo.events) != 1 || len(repo.notifications) != 2 {
		t.Fatalf("event/outbox coupling failed: %d %d %d", sender.calls, len(repo.events), len(repo.notifications))
	}
	repo.evaluations[0].CurrentValue = 20
	if err := s.EvaluateMetricAlerts(context.Background()); err != nil || !repo.resolved {
		t.Fatal("incident not resolved")
	}
}
func TestDeliveryFailureDoesNotBlockOtherRecipients(t *testing.T) {
	repo := &fakeAlertsRepository{pending: []*models.EmailNotification{{ID: 1, Attempts: 1}, {ID: 2, Attempts: 1}}}
	sender := &fakeEmailSender{}
	if err := NewAlertsService(repo, sender).DeliverNotifications(context.Background()); err == nil {
		t.Fatal("expected delivery failure")
	}
	if sender.calls != 2 || repo.completed[1] == nil || repo.completed[2] != nil {
		t.Fatal("recipients must be processed independently")
	}
}
