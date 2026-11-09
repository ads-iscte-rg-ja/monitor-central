package handlers

import (
	"context"
	"encoding/json"
	"io"
	"monitor-central/internal/models"
	"monitor-central/internal/services"
	"monitor-central/internal/utils/middleware"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ingestionRepo struct {
	stat       *models.MachineStat
	batchCalls int
}

func (f *ingestionRepo) GetHostByAPIKey(_ context.Context, key string) (*models.Host, error) {
	if key != "valid" {
		return nil, models.ErrHostNotFound
	}
	return &models.Host{ID: 7}, nil
}
func (f *ingestionRepo) CreateMachineStats(_ context.Context, _ int64, stats []*models.MachineStat) error {
	f.batchCalls++
	for i, s := range stats {
		f.stat = s
		s.ID = int64(i + 1)
	}
	return nil
}
func (f *ingestionRepo) CreateErrorLogs(_ context.Context, _ int64, logs []*models.ErrorLog) (int, error) {
	f.batchCalls++
	return len(logs), nil
}

type readSpy struct{ read bool }

func (s *readSpy) Read([]byte) (int, error) { s.read = true; return 0, io.EOF }
func (s *readSpy) Close() error             { return nil }
func TestKeyRejectedWithoutReadingBody(t *testing.T) {
	repo := &ingestionRepo{}
	service := services.NewIngestionService(repo)
	h := middleware.RequireHost(service)(http.HandlerFunc(NewIngestionHandler(service).IngestStats))
	for _, key := range []string{"", "invalid"} {
		body := &readSpy{}
		r := httptest.NewRequest("POST", "/api/v1/ingest/stats", nil)
		r.Body = body
		r.Header.Set("X-API-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || body.read {
			t.Fatalf("key=%q status=%d body read=%v", key, w.Code, body.read)
		}
	}
}
func TestIngestionTransportAndHostScope(t *testing.T) {
	repo := &ingestionRepo{}
	service := services.NewIngestionService(repo)
	handler := middleware.RequireHost(service)(http.HandlerFunc(NewIngestionHandler(service).IngestStats))
	stat := `{"id":1,"cpu_percent":50,"ram_used_mb":1,"ram_total_mb":2,"disk_used_gb":1,"disk_total_gb":2}`
	valid := `{"stats":[` + stat + `]}`
	for _, test := range []struct {
		body   string
		status int
	}{
		{valid, 201},
		{`{"stats":[` + strings.TrimSuffix(stat, "}") + `,"host_id":9}]}`, 400},
		{valid + ` {}`, 400},
		{`{"stats":[` + strings.TrimSuffix(stat, "}") + `,"collected_at":"2999-01-01T00:00:00Z"}]}`, 400},
		{`{"stats":[` + strings.TrimSuffix(stat, "}") + `,"padding":"` + strings.Repeat("x", 2*1024*1024) + `"}]}`, 413},
	} {
		r := httptest.NewRequest("POST", "/api/v1/ingest/stats", strings.NewReader(test.body))
		r.Header.Set("X-API-Key", "valid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("want %d got %d: %s", test.status, w.Code, w.Body)
		}
	}
	if repo.stat == nil || repo.stat.HostID != 7 {
		t.Fatal("request not bound to authenticated host")
	}
	// Known fields can exhaust the limit too, without unknown-field short circuit.
	r := httptest.NewRequest("POST", "/api/v1/ingest/errors", strings.NewReader(`{"errors":[{"id":1,"message":"`+strings.Repeat("x", 2*1024*1024)+`"}]}`))
	r.Header.Set("X-API-Key", "valid")
	w := httptest.NewRecorder()
	middleware.RequireHost(service)(http.HandlerFunc(NewIngestionHandler(service).IngestErrors)).ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("want 413 got %d", w.Code)
	}
}

func TestHTTPBatchesAcknowledgeEverySourceID(t *testing.T) {
	for _, kind := range []string{"stats", "errors"} {
		t.Run(kind, func(t *testing.T) {
			repo := &ingestionRepo{}
			s := services.NewIngestionService(repo)
			h := NewIngestionHandler(s)
			var input any
			endpoint := h.IngestStats
			if kind == "stats" {
				request := models.MachineStatsBatchRequest{}
				for id := int64(100); id > 0; id-- {
					request.Stats = append(request.Stats, models.MachineStatsRequest{ID: id, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2})
				}
				input = request
			} else {
				request := models.ErrorLogsRequest{}
				for id := int64(100); id > 0; id-- {
					request.Errors = append(request.Errors, models.ErrorLogInput{ID: id, AppName: "app", Environment: "test", Level: "ERROR", Message: "failure"})
				}
				input, endpoint = request, h.IngestErrors
			}
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/v1/ingest/"+kind, strings.NewReader(string(body)))
			r.Header.Set("X-API-Key", "valid")
			w := httptest.NewRecorder()
			middleware.RequireHost(s)(http.HandlerFunc(endpoint)).ServeHTTP(w, r)
			if w.Code != 201 || repo.batchCalls != 1 {
				t.Fatal("batch was not committed once", w.Code, w.Body)
			}
			var ack struct {
				Data      []models.StatAcknowledgement `json:"data"`
				SourceIDs []int64                      `json:"source_ids"`
				Accepted  int                          `json:"accepted"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &ack); err != nil {
				t.Fatal(err)
			}
			if kind == "stats" {
				if len(ack.Data) != 100 {
					t.Fatal("incomplete statistics acknowledgement")
				}
				for i, entry := range ack.Data {
					if entry.SourceID != int64(100-i) || entry.ID <= 0 || entry.HostID != 7 {
						t.Fatal("wrong acknowledgement", entry)
					}
				}
			} else {
				if ack.Accepted != 100 || len(ack.SourceIDs) != 100 {
					t.Fatal("incomplete error acknowledgement")
				}
				for i, id := range ack.SourceIDs {
					if id != int64(100-i) {
						t.Fatal("wrong acknowledged source ID", id)
					}
				}
			}
		})
	}
}

func TestRejectedBatchNamesOnlyInvalidRecord(t *testing.T) {
	repo := &ingestionRepo{}
	s := services.NewIngestionService(repo)
	h := NewIngestionHandler(s)
	for _, tc := range []struct {
		body     string
		endpoint http.HandlerFunc
	}{
		{`{"stats":[{"id":2,"cpu_percent":25,"ram_used_mb":1,"ram_total_mb":2,"disk_used_gb":1,"disk_total_gb":2},{"id":1,"cpu_percent":101}]}`, h.IngestStats},
		{`{"errors":[{"id":2,"app_name":"app","environment":"test","level":"ERROR","message":"valid"},{"id":1,"app_name":"app","environment":"test","level":"ERROR","message":""}]}`, h.IngestErrors},
	} {
		r := httptest.NewRequest("POST", "/api/v1/ingest", strings.NewReader(tc.body))
		r.Header.Set("X-API-Key", "valid")
		w := httptest.NewRecorder()
		middleware.RequireHost(s)(tc.endpoint).ServeHTTP(w, r)
		var rejection struct {
			Error    string `json:"error"`
			SourceID int64  `json:"source_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rejection); err != nil || w.Code != 400 || rejection.Error != "invalid_data" || rejection.SourceID != 1 || repo.batchCalls != 0 {
			t.Fatal("batch rejection is not record-specific/atomic", w.Code, w.Body, err)
		}
	}
}
