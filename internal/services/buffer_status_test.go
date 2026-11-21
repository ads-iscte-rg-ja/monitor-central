package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"monitor-central/internal/models"
)

func TestValidateBufferStatus(t *testing.T) {
	now := time.Now()
	valid := func() *models.BufferStatus {
		return &models.BufferStatus{ObservedAt: now.Add(-time.Second), Pending: 1, Quarantined: 2,
			OldestPendingAgeSeconds: 3, SizeBytes: 4, DroppedByKind: map[string]int64{"INFO": 5, "machine_stat": 6}}
	}
	if err := ValidateBufferStatus(valid(), now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*models.BufferStatus){
		"missing time":     func(b *models.BufferStatus) { b.ObservedAt = time.Time{} },
		"future time":      func(b *models.BufferStatus) { b.ObservedAt = now.Add(time.Second) },
		"negative pending": func(b *models.BufferStatus) { b.Pending = -1 },
		"negative quarant": func(b *models.BufferStatus) { b.Quarantined = -1 },
		"negative age":     func(b *models.BufferStatus) { b.OldestPendingAgeSeconds = -1 },
		"negative size":    func(b *models.BufferStatus) { b.SizeBytes = -1 },
		"unknown kind":     func(b *models.BufferStatus) { b.DroppedByKind["DEBUG"] = 1 },
		"negative drops":   func(b *models.BufferStatus) { b.DroppedByKind["WARN"] = -1 },
	} {
		status := valid()
		change(status)
		if err := ValidateBufferStatus(status, now); !errors.Is(err, models.ErrInvalidBufferStatus) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStatisticsBatchKeepsOnlyAValidBufferStatus(t *testing.T) {
	ctx := context.Background()
	batch := func(buffer *models.BufferStatus) *models.MachineStatsBatchRequest {
		stat := *validStats()
		stat.ID = 1
		return &models.MachineStatsBatchRequest{Stats: []models.MachineStatsRequest{stat}, Buffer: buffer}
	}
	repo := &fakeIngestionRepository{}
	valid := &models.BufferStatus{ObservedAt: time.Now().Add(-time.Second), Quarantined: 3}
	if _, err := NewIngestionService(repo).IngestStatsBatch(ctx, 7, batch(valid)); err != nil || repo.buffer != valid {
		t.Fatalf("valid buffer status not passed on: %v %v", repo.buffer, err)
	}
	invalid := &models.BufferStatus{ObservedAt: time.Now().Add(-time.Second), Pending: -1}
	if _, err := NewIngestionService(repo).IngestStatsBatch(ctx, 7, batch(invalid)); err != nil || repo.buffer != nil || len(repo.stats) != 1 {
		t.Fatalf("invalid buffer status must be dropped, the statistics kept: %v %v", repo.buffer, err)
	}
}

func TestMetricsIncludeTheSelectedHostsBuffer(t *testing.T) {
	ctx := context.Background()
	status := &models.BufferStatus{Quarantined: 3}
	repo := &fakeDashboardRepository{hosts: []models.DashboardHost{{ID: 1, Name: "a"}}, buffer: status}
	metrics, err := NewDashboardService(repo).GetMetrics(ctx, 1, "24h")
	if err != nil || metrics.Buffer != status {
		t.Fatalf("buffer %v (%v)", metrics, err)
	}
	repo.bufferErr = errors.New("connection reset")
	if _, err := NewDashboardService(repo).GetMetrics(ctx, 1, "24h"); !errors.Is(err, repo.bufferErr) {
		t.Fatalf("buffer error not reported: %v", err)
	}
}
