package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"monitor-central/internal/models"
)

type fakeErrorLogs struct {
	filters models.ErrorLogFilters
	err     error
}

func (f *fakeErrorLogs) GetPage(_ context.Context, filters models.ErrorLogFilters) (*models.ErrorLogsPage, error) {
	f.filters = filters
	if f.err != nil {
		return nil, f.err
	}
	return &models.ErrorLogsPage{
		Errors:     []models.ErrorLogListItem{{ID: 1, HostName: "web-1", AppName: "api", Level: models.LevelError, Message: "timeout", Metadata: []byte(`{"try":2}`)}},
		Page:       filters.Page,
		PageSize:   20,
		TotalPages: 3,
		Filters:    filters,
	}, nil
}

func TestErrorLogsFiltersAndPagination(t *testing.T) {
	svc := &fakeErrorLogs{}
	h := NewErrorLogsHandler(svc, realPages(t), discard())
	w := call(t, "GET", "/errors", "/errors?page=2&host_id=5&level=+error+&app_name=api&search=+time+", nil, h.GetErrorLogs)
	wantStatus(t, w, http.StatusOK, "timeout")
	if f := svc.filters; f.Page != 2 || f.HostID != 5 || f.Level != models.LevelError || f.AppName != "api" || f.Search != "time" || f.PageSize != 20 {
		t.Fatalf("filters %+v", f)
	}
	// Both neighbours keep the filters (the page escapes & in the template).
	body := w.Body.String()
	if !strings.Contains(body, "page=1") || !strings.Contains(body, "page=3") || !strings.Contains(body, "level=ERROR") {
		t.Fatalf("pagination links missing: %s", body)
	}

	_ = call(t, "GET", "/errors", "/errors?page=-4", nil, h.GetErrorLogs)
	if svc.filters.Page != 1 {
		t.Fatalf("invalid page kept: %d", svc.filters.Page)
	}
	wantStatus(t, call(t, "GET", "/errors", "/errors?host_id=x", nil, h.GetErrorLogs), http.StatusBadRequest, "Invalid host ID")
	svc.err = models.ErrInvalidErrorLevel
	wantStatus(t, call(t, "GET", "/errors", "/errors?level=DEBUG", nil, h.GetErrorLogs), http.StatusBadRequest, "Invalid error level")
	svc.err = errStore
	wantStatus(t, call(t, "GET", "/errors", "/errors", nil, h.GetErrorLogs), http.StatusInternalServerError, "Internal Server Error")
}
