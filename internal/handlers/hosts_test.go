package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"monitor-central/internal/models"
)

type fakeHosts struct {
	page    *models.HostsPage
	pageErr error
	err     error
	created models.CreateHostRequest
	updated models.UpdateHostRequest
	id      int64
}

func (f *fakeHosts) GetPage(context.Context) (*models.HostsPage, error) { return f.page, f.pageErr }
func (f *fakeHosts) CreateHost(_ context.Context, r models.CreateHostRequest) (*models.Host, error) {
	f.created = r
	if f.err != nil {
		return nil, f.err
	}
	return &models.Host{Name: r.Name, APIKey: "new-key-shown-once"}, nil
}
func (f *fakeHosts) UpdateHost(_ context.Context, id int64, r models.UpdateHostRequest) error {
	f.id, f.updated = id, r
	return f.err
}
func (f *fakeHosts) DeleteHost(_ context.Context, id int64) error { f.id = id; return f.err }
func (f *fakeHosts) RegenerateAPIKey(_ context.Context, id int64) (string, error) {
	f.id = id
	return "regenerated-key", f.err
}

func hostsPage() *models.HostsPage {
	return &models.HostsPage{Hosts: []models.HostListItem{{ID: 1, Name: "web-1", Hostname: "web-1.local", Status: "online", CreatedAt: time.Now(), LastSeen: "Never", APIKeyMasked: "abcd...wxyz"}}}
}

func TestHostsPageAndNewKeysAreShownOnce(t *testing.T) {
	svc := &fakeHosts{page: hostsPage()}
	h := NewHostsHandler(svc, realPages(t), discard())
	wantStatus(t, call(t, "GET", "/hosts", "/hosts", nil, h.GetHosts), http.StatusOK, "web-1")

	form := url.Values{"name": {"db-1"}, "hostname": {"db-1.local"}, "health_endpoint": {"https://db-1/health"}, "contact_timeout_minutes": {"10"}}
	wantStatus(t, call(t, "POST", "/hosts", "/hosts", form, h.CreateHost), http.StatusOK, "new-key-shown-once")
	if svc.created.Name != "db-1" || svc.created.ContactTimeoutMinutes != 10 || svc.created.HealthEndpoint != "https://db-1/health" {
		t.Fatalf("request %+v", svc.created)
	}
	wantStatus(t, call(t, "POST", "/hosts/{hostID}/regenerate-key", "/hosts/4/regenerate-key", nil, h.RegenerateAPIKey), http.StatusOK, "regenerated-key")
	if svc.id != 4 {
		t.Fatalf("id %d", svc.id)
	}

	svc.pageErr = errStore
	wantStatus(t, call(t, "GET", "/hosts", "/hosts", nil, h.GetHosts), http.StatusInternalServerError, "Internal Server Error")
}

func TestHostFormsMapErrors(t *testing.T) {
	svc := &fakeHosts{page: hostsPage()}
	h := NewHostsHandler(svc, nil, discard())
	form := url.Values{"name": {"db-1"}, "hostname": {"db-1.local"}}
	badTimeout := url.Values{"name": {"db-1"}, "contact_timeout_minutes": {"0"}}

	wantStatus(t, call(t, "POST", "/hosts", "/hosts", badTimeout, h.CreateHost), http.StatusBadRequest, models.ErrContactTimeout.Error())
	for err, status := range map[error]int{models.ErrHostNameRequired: 400, models.ErrHealthEndpoint: 400, errStore: 500} {
		svc.err = err
		if w := call(t, "POST", "/hosts", "/hosts", form, h.CreateHost); w.Code != status {
			t.Fatalf("create %v: %d, want %d", err, w.Code, status)
		}
	}

	const edit = "/hosts/{hostID}/edit"
	svc.err = nil
	wantRedirect(t, call(t, "POST", edit, "/hosts/2/edit", form, h.UpdateHost), "/hosts")
	if svc.id != 2 || svc.updated.ContactTimeoutMinutes != 5 { // empty timeout: the 5-minute default
		t.Fatalf("id %d request %+v", svc.id, svc.updated)
	}
	wantStatus(t, call(t, "POST", edit, "/hosts/x/edit", form, h.UpdateHost), http.StatusBadRequest, "Invalid host ID")
	wantStatus(t, call(t, "POST", edit, "/hosts/2/edit", badTimeout, h.UpdateHost), http.StatusBadRequest, models.ErrContactTimeout.Error())
	for err, status := range map[error]int{models.ErrHostnameRequired: 400, models.ErrHostNotFound: 404, errStore: 500} {
		svc.err = err
		if w := call(t, "POST", edit, "/hosts/2/edit", form, h.UpdateHost); w.Code != status {
			t.Fatalf("update %v: %d, want %d", err, w.Code, status)
		}
	}

	svc.err = nil
	wantRedirect(t, call(t, "POST", "/hosts/{hostID}/delete", "/hosts/3/delete", nil, h.DeleteHost), "/hosts")
	for pattern, handler := range map[string]http.HandlerFunc{
		"/hosts/{hostID}/delete":         h.DeleteHost,
		"/hosts/{hostID}/regenerate-key": h.RegenerateAPIKey,
	} {
		svc.err = nil
		wantStatus(t, call(t, "POST", pattern, strings.Replace(pattern, "{hostID}", "0", 1), nil, handler), http.StatusBadRequest, "Invalid host ID")
		for err, status := range map[error]int{models.ErrHostIDRequired: 400, models.ErrHostNotFound: 404, errStore: 500} {
			svc.err = err
			if w := call(t, "POST", pattern, strings.Replace(pattern, "{hostID}", "3", 1), nil, handler); w.Code != status {
				t.Fatalf("%s %v: %d, want %d", pattern, err, w.Code, status)
			}
		}
	}
}
