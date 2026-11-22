package services

import (
	"context"
	"errors"
	"monitor-central/internal/models"
	"strings"
	"testing"
)

// The validation limits are fixed at their exact value: each one is tested at
// the limit (accepted) and one step past it (rejected). Mutation testing showed
// that the other tests only used values far from the limits, so a ">" turned
// into ">=" went unnoticed (docs/metrics/iter-7/mutacao.md in monitor-platform).

func TestAPIKeyLengthLimit(t *testing.T) {
	host := &models.Host{ID: 7}
	s := NewIngestionService(&fakeIngestionRepository{host: host})
	for _, tc := range []struct {
		name string
		key  string
		ok   bool
	}{
		{"at limit", strings.Repeat("k", 256), true},
		{"past limit", strings.Repeat("k", 257), false},
		{"one character", "k", true},
		{"blank", "   ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.AuthenticateHost(context.Background(), tc.key)
			if tc.ok && (err != nil || got != host) {
				t.Fatalf("want host, got %v %v", got, err)
			}
			if !tc.ok && !errors.Is(err, models.ErrInvalidAPIKey) {
				t.Fatalf("want ErrInvalidAPIKey, got %v", err)
			}
		})
	}
}

func TestHostIDMustBePositive(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		hostID int64
		ok     bool
	}{{0, false}, {1, true}} {
		s := NewIngestionService(&fakeIngestionRepository{})
		_, err := s.IngestStats(ctx, tc.hostID, validStats())
		expectHostResult(t, "IngestStats", tc.hostID, tc.ok, err, models.ErrInvalidAPIKey)

		batch := &models.MachineStatsBatchRequest{Stats: []models.MachineStatsRequest{*validStats()}}
		batch.Stats[0].ID = 1
		_, err = s.IngestStatsBatch(ctx, tc.hostID, batch)
		expectHostResult(t, "IngestStatsBatch", tc.hostID, tc.ok, err, models.ErrInvalidAPIKey)

		_, _, err = s.IngestErrors(ctx, tc.hostID, &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{validLog()}})
		expectHostResult(t, "IngestErrors", tc.hostID, tc.ok, err, models.ErrInvalidAPIKey)
	}
}

func TestStatsRangeLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.MachineStatsRequest)
		want   error // nil: accepted
	}{
		{"cpu 0", func(s *models.MachineStatsRequest) { s.CPUPercent = 0 }, nil},
		{"cpu 100", func(s *models.MachineStatsRequest) { s.CPUPercent = 100 }, nil},
		{"cpu just below 0", func(s *models.MachineStatsRequest) { s.CPUPercent = -0.001 }, models.ErrInvalidCPUPercent},
		{"cpu just above 100", func(s *models.MachineStatsRequest) { s.CPUPercent = 100.001 }, models.ErrInvalidCPUPercent},
		{"ram used 0", func(s *models.MachineStatsRequest) { s.RAMUsedMB = 0 }, nil},
		{"ram used equals total", func(s *models.MachineStatsRequest) { s.RAMUsedMB = s.RAMTotalMB }, nil},
		{"ram used just above total", func(s *models.MachineStatsRequest) { s.RAMUsedMB = s.RAMTotalMB + 0.001 }, models.ErrInvalidRAMUsage},
		{"ram used just below 0", func(s *models.MachineStatsRequest) { s.RAMUsedMB = -0.001 }, models.ErrInvalidRAMUsage},
		{"ram total 0 and used 0", func(s *models.MachineStatsRequest) { s.RAMUsedMB, s.RAMTotalMB = 0, 0 }, models.ErrInvalidRAMUsage},
		{"disk used 0", func(s *models.MachineStatsRequest) { s.DiskUsedGB = 0 }, nil},
		{"disk used equals total", func(s *models.MachineStatsRequest) { s.DiskUsedGB = s.DiskTotalGB }, nil},
		{"disk used just above total", func(s *models.MachineStatsRequest) { s.DiskUsedGB = s.DiskTotalGB + 0.001 }, models.ErrInvalidDiskUsage},
		{"disk total 0 and used 0", func(s *models.MachineStatsRequest) { s.DiskUsedGB, s.DiskTotalGB = 0, 0 }, models.ErrInvalidDiskUsage},
		{"source id -1", func(s *models.MachineStatsRequest) { s.ID = -1 }, models.ErrMissingStatID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := validStats()
			tc.change(request)
			stat, err := NewIngestionService(&fakeIngestionRepository{}).IngestStats(context.Background(), 7, request)
			if tc.want == nil && (err != nil || stat == nil) {
				t.Fatalf("want accepted, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// Source ID 0 is a sample without a source (the single-sample API); 1 is the
// first valid source ID.
func TestStatsSourceIDBoundary(t *testing.T) {
	s := NewIngestionService(&fakeIngestionRepository{})
	request := validStats()
	stat, err := s.IngestStats(context.Background(), 7, request)
	if err != nil || stat.SourceID != nil {
		t.Fatalf("source ID 0 must store no source, got %+v %v", stat, err)
	}
	request.ID = 1
	stat, err = s.IngestStats(context.Background(), 7, request)
	if err != nil || stat.SourceID == nil || *stat.SourceID != 1 {
		t.Fatalf("source ID 1 must be stored, got %+v %v", stat, err)
	}
}

func TestErrorBatchSizeLimit(t *testing.T) {
	logs := make([]models.ErrorLogInput, MaxErrorBatch)
	for i := range logs {
		logs[i] = validLog()
		logs[i].ID = int64(i + 1)
	}
	accepted, _, err := NewIngestionService(&fakeIngestionRepository{}).IngestErrors(context.Background(), 7, &models.ErrorLogsRequest{Errors: logs})
	if err != nil || accepted != MaxErrorBatch {
		t.Fatalf("a batch of exactly %d must be accepted, got %d %v", MaxErrorBatch, accepted, err)
	}
	// One more is covered by TestErrorsHostScopeAndValidation (101 entries).
}

func TestErrorFieldSizeLimits(t *testing.T) {
	stack := func(n int) func(*models.ErrorLogInput) {
		return func(l *models.ErrorLogInput) { v := strings.Repeat("s", n); l.StackTrace = &v }
	}
	// Valid JSON of exactly n bytes: a string literal.
	metadata := func(n int) func(*models.ErrorLogInput) {
		return func(l *models.ErrorLogInput) { l.Metadata = []byte(`"` + strings.Repeat("m", n-2) + `"`) }
	}
	for _, tc := range []struct {
		name   string
		limit  int
		change func(n int) func(*models.ErrorLogInput)
	}{
		{"app name", 200, func(n int) func(*models.ErrorLogInput) {
			return func(l *models.ErrorLogInput) { l.AppName = strings.Repeat("a", n) }
		}},
		{"environment", 100, func(n int) func(*models.ErrorLogInput) {
			return func(l *models.ErrorLogInput) { l.Environment = strings.Repeat("e", n) }
		}},
		{"message", 16384, func(n int) func(*models.ErrorLogInput) {
			return func(l *models.ErrorLogInput) { l.Message = strings.Repeat("x", n) }
		}},
		{"stack trace", 65536, stack},
		{"metadata", 32768, metadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []int{tc.limit, tc.limit + 1} {
				entry := validLog()
				tc.change(n)(&entry)
				_, _, err := NewIngestionService(&fakeIngestionRepository{}).IngestErrors(context.Background(), 7, &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{entry}})
				if n == tc.limit && err != nil {
					t.Fatalf("%d bytes (the limit) must be accepted, got %v", n, err)
				}
				if n > tc.limit && !errors.Is(err, models.ErrFieldTooLarge) {
					t.Fatalf("%d bytes must be rejected as too large, got %v", n, err)
				}
			}
		})
	}
}

func validLog() models.ErrorLogInput {
	return models.ErrorLogInput{ID: 1, AppName: "app", Environment: "prod", Level: "ERROR", Message: "failure"}
}

func expectHostResult(t *testing.T, method string, hostID int64, ok bool, err, want error) {
	t.Helper()
	if ok && err != nil {
		t.Fatalf("%s with host %d: want accepted, got %v", method, hostID, err)
	}
	if !ok && !errors.Is(err, want) {
		t.Fatalf("%s with host %d: want %v, got %v", method, hostID, want, err)
	}
}
