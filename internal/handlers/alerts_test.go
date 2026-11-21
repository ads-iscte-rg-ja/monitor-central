package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"monitor-central/internal/models"
)

type fakeAlerts struct {
	page    *models.AlertsPage
	err     error
	created models.CreateAlertRequest
	updated models.UpdateAlertRequest
	id      int64
}

func (f *fakeAlerts) GetPage(context.Context) (*models.AlertsPage, error) { return f.page, f.err }
func (f *fakeAlerts) CreateAlert(_ context.Context, r models.CreateAlertRequest) (*models.AlertRule, error) {
	f.created = r
	return &models.AlertRule{}, f.err
}
func (f *fakeAlerts) UpdateAlert(_ context.Context, id int64, r models.UpdateAlertRequest) error {
	f.id, f.updated = id, r
	return f.err
}
func (f *fakeAlerts) DeleteAlert(_ context.Context, id int64) error { f.id = id; return f.err }
func (f *fakeAlerts) AcknowledgeAlertEvent(_ context.Context, id int64) error {
	f.id = id
	return f.err
}

func alertFormValues() url.Values {
	return url.Values{"name": {"CPU"}, "host_id": {"3"}, "metric": {"cpu"}, "condition": {"gt"}, "threshold": {"90"},
		"severity": {"critical"}, "channels": {"email", "dashboard"}, "enabled": {"on"}}
}

func TestAlertsPageRendersRulesAndEvents(t *testing.T) {
	host := int64(3)
	svc := &fakeAlerts{page: &models.AlertsPage{
		Alerts:      []models.AlertRuleListItem{{ID: 1, Name: "CPU alta", HostID: &host, Metric: "cpu", Condition: "gt", Threshold: 90, ChannelsCSV: "email", Enabled: true}},
		AlertEvents: []models.AlertEventListItem{{ID: 2, AlertName: "CPU alta", HostName: "web-1", Status: "fired", FiredAtDisplay: "21 Nov 2026, 10:00"}},
		Hosts:       []models.AlertHostOption{{ID: 3, Name: "web-1", Hostname: "web-1.local"}},
	}}
	h := NewAlertsHandler(svc, realPages(t), discard())
	wantStatus(t, call(t, "GET", "/alerts", "/alerts", nil, h.GetAlerts), http.StatusOK, "CPU alta")

	svc.err = errStore
	wantStatus(t, call(t, "GET", "/alerts", "/alerts", nil, h.GetAlerts), http.StatusInternalServerError, "Internal Server Error")
}

func TestCreateAlertPassesTheFormAndMapsErrors(t *testing.T) {
	svc := &fakeAlerts{}
	h := NewAlertsHandler(svc, nil, discard())
	wantRedirect(t, call(t, "POST", "/alerts", "/alerts", alertFormValues(), h.CreateAlert), "/alerts")
	if got := svc.created; got.Name != "CPU" || got.HostID != 3 || got.Threshold != 90 || !got.Enabled || len(got.Channels) != 2 {
		t.Fatalf("request %+v", got)
	}

	bad := alertFormValues()
	bad.Set("threshold", "high")
	wantStatus(t, call(t, "POST", "/alerts", "/alerts", bad, h.CreateAlert), http.StatusBadRequest, models.ErrInvalidAlertThreshold.Error())
	bad = alertFormValues()
	bad.Set("host_id", "x")
	wantStatus(t, call(t, "POST", "/alerts", "/alerts", bad, h.CreateAlert), http.StatusBadRequest, "Invalid host ID")

	for err, status := range map[error]int{models.ErrInvalidAlertMetric: 400, models.ErrAlertChannelRequired: 400, errStore: 500} {
		svc.err = err
		w := call(t, "POST", "/alerts", "/alerts", alertFormValues(), h.CreateAlert)
		if w.Code != status {
			t.Fatalf("%v: status %d, want %d", err, w.Code, status)
		}
	}
}

func TestUpdateAlertPassesTheIDAndMapsErrors(t *testing.T) {
	svc := &fakeAlerts{}
	h := NewAlertsHandler(svc, nil, discard())
	const pattern = "/alerts/{alertID}/edit"
	wantRedirect(t, call(t, "POST", pattern, "/alerts/5/edit", alertFormValues(), h.UpdateAlert), "/alerts")
	if svc.id != 5 || svc.updated.Metric != "cpu" {
		t.Fatalf("id %d request %+v", svc.id, svc.updated)
	}
	wantStatus(t, call(t, "POST", pattern, "/alerts/x/edit", alertFormValues(), h.UpdateAlert), http.StatusBadRequest, "Invalid alert ID")
	bad := alertFormValues()
	bad.Set("threshold", "")
	wantStatus(t, call(t, "POST", pattern, "/alerts/5/edit", bad, h.UpdateAlert), http.StatusBadRequest, models.ErrInvalidAlertThreshold.Error())
	bad = alertFormValues()
	bad.Del("host_id")
	wantStatus(t, call(t, "POST", pattern, "/alerts/5/edit", bad, h.UpdateAlert), http.StatusBadRequest, "Invalid host ID")
	for err, status := range map[error]int{models.ErrAlertNameRequired: 400, models.ErrAlertNotFound: 404, errStore: 500} {
		svc.err = err
		if w := call(t, "POST", pattern, "/alerts/5/edit", alertFormValues(), h.UpdateAlert); w.Code != status {
			t.Fatalf("%v: status %d, want %d", err, w.Code, status)
		}
	}
}

func TestDeleteAndAcknowledgeMapErrors(t *testing.T) {
	svc := &fakeAlerts{}
	h := NewAlertsHandler(svc, nil, discard())
	for _, tc := range []struct {
		name, pattern, target, bad string
		handler                    http.HandlerFunc
		errors                     map[error]int
	}{
		{"delete", "/alerts/{alertID}/delete", "/alerts/%s/delete", "Invalid alert ID", h.DeleteAlert,
			map[error]int{models.ErrAlertIDRequired: 400, models.ErrAlertNotFound: 404, errStore: 500}},
		{"acknowledge", "/alerts/events/{eventID}/acknowledge", "/alerts/events/%s/acknowledge", "Invalid event ID", h.AcknowledgeAlertEvent,
			map[error]int{models.ErrAlertEventIDRequired: 400, models.ErrAlertEventNotFound: 404, errStore: 500}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc.err = nil
			wantRedirect(t, call(t, "POST", tc.pattern, fmt.Sprintf(tc.target, "9"), nil, tc.handler), "/alerts")
			if svc.id != 9 {
				t.Fatalf("id %d", svc.id)
			}
			wantStatus(t, call(t, "POST", tc.pattern, fmt.Sprintf(tc.target, "0"), nil, tc.handler), http.StatusBadRequest, tc.bad)
			for err, status := range tc.errors {
				svc.err = err
				if w := call(t, "POST", tc.pattern, fmt.Sprintf(tc.target, "9"), nil, tc.handler); w.Code != status {
					t.Fatalf("%v: status %d, want %d", err, w.Code, status)
				}
			}
		})
	}
}
