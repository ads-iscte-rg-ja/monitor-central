package services

import (
	"context"
	"errors"
	"math"
	"monitor-central/internal/models"
	"testing"
	"time"
)

type fakeIngestionRepository struct {
	host   *models.Host
	err    error
	stat   *models.MachineStat
	stats  []*models.MachineStat
	logs   []*models.ErrorLog
	hostID int64
}

func (f *fakeIngestionRepository) GetHostByAPIKey(context.Context, string) (*models.Host, error) {
	return f.host, f.err
}
func (f *fakeIngestionRepository) CreateMachineStats(_ context.Context, hostID int64, stats []*models.MachineStat) error {
	for i, s := range stats {
		s.ID = int64(12 + i)
		f.stat = s
	}
	f.stats = stats
	f.hostID = hostID
	return nil
}

func TestStatisticsBatchValidationAndHostScope(t *testing.T) {
	ctx := context.Background()
	request := &models.MachineStatsBatchRequest{}
	for id := int64(100); id > 0; id-- {
		stat := *validStats()
		stat.ID = id
		request.Stats = append(request.Stats, stat)
	}
	repo := &fakeIngestionRepository{}
	ack, err := NewIngestionService(repo).IngestStatsBatch(ctx, 7, request)
	if err != nil || len(ack) != 100 || len(repo.stats) != 100 || repo.hostID != 7 {
		t.Fatal("complete batch was not persisted", err)
	}
	for i, entry := range ack {
		if entry.SourceID != int64(100-i) || entry.HostID != 7 || entry.ID != int64(12+i) {
			t.Fatal("source ID or batch ordering changed", entry)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*models.MachineStatsBatchRequest)
		want   error
	}{
		{"empty", func(r *models.MachineStatsBatchRequest) { r.Stats = nil }, models.ErrEmptyStatsBatch},
		{"oversized", func(r *models.MachineStatsBatchRequest) { r.Stats = append(r.Stats, r.Stats[0]) }, models.ErrStatsBatchTooLarge},
		{"duplicate", func(r *models.MachineStatsBatchRequest) { r.Stats[99].ID = r.Stats[0].ID }, models.ErrDuplicateSourceID},
		{"missing source", func(r *models.MachineStatsBatchRequest) { r.Stats[99].ID = 0 }, models.ErrMissingStatID},
		{"invalid last member", func(r *models.MachineStatsBatchRequest) { r.Stats[99].CPUPercent = 101 }, models.ErrInvalidCPUPercent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &models.MachineStatsBatchRequest{Stats: append([]models.MachineStatsRequest(nil), request.Stats...)}
			tc.change(input)
			repo := &fakeIngestionRepository{}
			_, err := NewIngestionService(repo).IngestStatsBatch(ctx, 7, input)
			if !errors.Is(err, tc.want) || repo.stats != nil {
				t.Fatal("invalid batch partially persisted", err)
			}
			if tc.name == "invalid last member" {
				var rejection *models.RecordValidationError
				if !errors.As(err, &rejection) || rejection.SourceID != 1 {
					t.Fatal("rejection omitted the invalid source ID", err)
				}
			}
		})
	}
}
func (f *fakeIngestionRepository) CreateErrorLogs(_ context.Context, id int64, logs []*models.ErrorLog) (int, error) {
	f.hostID = id
	f.logs = logs
	return len(logs), nil
}
func TestIngestionHostAuthentication(t *testing.T) {
	s := NewIngestionService(&fakeIngestionRepository{err: models.ErrHostNotFound})
	for _, key := range []string{"", "wrong"} {
		if _, err := s.AuthenticateHost(context.Background(), key); !errors.Is(err, models.ErrInvalidAPIKey) {
			t.Fatal(err)
		}
	}
}
func validStats() *models.MachineStatsRequest {
	return &models.MachineStatsRequest{CPUPercent: 45, RAMUsedMB: 4096, RAMTotalMB: 8192, DiskUsedGB: 120, DiskTotalGB: 500}
}
func TestStatsValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*models.MachineStatsRequest)
		want   error
	}{
		{"negative cpu", func(s *models.MachineStatsRequest) { s.CPUPercent = -1 }, models.ErrInvalidCPUPercent},
		{"high cpu", func(s *models.MachineStatsRequest) { s.CPUPercent = 101 }, models.ErrInvalidCPUPercent},
		{"nonfinite cpu", func(s *models.MachineStatsRequest) { s.CPUPercent = math.NaN() }, models.ErrInvalidCPUPercent},
		{"zero ram", func(s *models.MachineStatsRequest) { s.RAMTotalMB = 0 }, models.ErrInvalidRAMUsage},
		{"excess ram", func(s *models.MachineStatsRequest) { s.RAMUsedMB = 9000 }, models.ErrInvalidRAMUsage},
		{"negative disk", func(s *models.MachineStatsRequest) { s.DiskUsedGB = -1 }, models.ErrInvalidDiskUsage},
		{"future", func(s *models.MachineStatsRequest) { s.CollectedAt = time.Now().Add(time.Hour) }, models.ErrFutureTimestamp},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeIngestionRepository{}
			request := validStats()
			test.change(request)
			_, err := NewIngestionService(repo).IngestStats(context.Background(), 7, request)
			if !errors.Is(err, test.want) {
				t.Fatalf("want %v got %v", test.want, err)
			}
			if repo.stat != nil {
				t.Fatal("invalid sample persisted")
			}
		})
	}
	repo := &fakeIngestionRepository{}
	request := validStats()
	request.ID = 5
	s, err := NewIngestionService(repo).IngestStats(context.Background(), 7, request)
	if err != nil || s.ID != 12 || s.HostID != 7 || s.CollectedAt.IsZero() || *s.SourceID != 5 {
		t.Fatalf("unexpected stat %+v %v", s, err)
	}
}
func TestErrorsHostScopeAndValidation(t *testing.T) {
	valid := models.ErrorLogInput{ID: 1, AppName: " app ", Environment: " prod ", Level: "error", Message: "failure"}
	repo := &fakeIngestionRepository{}
	s := NewIngestionService(repo)
	accepted, inserted, err := s.IngestErrors(context.Background(), 9, &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{valid}})
	if err != nil || accepted != 1 || inserted != 1 || repo.hostID != 9 || repo.logs[0].HostID != 9 || *repo.logs[0].SourceID != 1 || repo.logs[0].Level != "ERROR" {
		t.Fatalf("scope/normalization failed %v", err)
	}
	for _, test := range []struct {
		change func(*models.ErrorLogInput)
		want   error
	}{
		{func(l *models.ErrorLogInput) { l.ID = 0 }, models.ErrMissingErrorID},
		{func(l *models.ErrorLogInput) { l.Level = "DEBUG" }, models.ErrInvalidErrorLevel},
		{func(l *models.ErrorLogInput) { l.CreatedAt = time.Now().Add(time.Minute) }, models.ErrFutureTimestamp},
		{func(l *models.ErrorLogInput) { l.Message = " " }, models.ErrMissingErrorMessage},
	} {
		entry := valid
		test.change(&entry)
		_, _, err := s.IngestErrors(context.Background(), 9, &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{entry}})
		if !errors.Is(err, test.want) {
			t.Fatalf("want %v got %v", test.want, err)
		}
	}
	if _, _, err := s.IngestErrors(context.Background(), 9, &models.ErrorLogsRequest{Errors: make([]models.ErrorLogInput, 101)}); !errors.Is(err, models.ErrBatchTooLarge) {
		t.Fatal(err)
	}
}
