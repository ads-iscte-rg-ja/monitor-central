//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
)

// MN1: a quarantine on a host reaches central with the next statistics batch
// and is shown by the dashboard API. Only the newest snapshot is kept, and an
// invalid one never costs the statistics.
func TestBufferStatusReachesTheDashboard(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "buffer-status")
	ingest := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	dashboard := services.NewDashboardService(repositories.NewDashboardRepository(db))
	observed := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	source := int64(0)
	send := func(buffer *models.BufferStatus) {
		t.Helper()
		source++
		request := &models.MachineStatsBatchRequest{
			Stats:  []models.MachineStatsRequest{{ID: source, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2}},
			Buffer: buffer,
		}
		if _, err := ingest.IngestStatsBatch(ctx, h.ID, request); err != nil {
			t.Fatal(err)
		}
	}
	buffer := func() *models.BufferStatus {
		t.Helper()
		metrics, err := dashboard.GetMetrics(ctx, h.ID, "24h")
		if err != nil {
			t.Fatal(err)
		}
		return metrics.Buffer
	}

	send(nil)
	if buffer() != nil {
		t.Fatal("a host that never reported its buffer must have none")
	}

	send(&models.BufferStatus{ObservedAt: observed, Pending: 12, Quarantined: 3, OldestPendingAgeSeconds: 40,
		SizeBytes: 1 << 20, DroppedByKind: map[string]int64{"INFO": 7}})
	got := buffer()
	if got == nil || got.Quarantined != 3 || got.Pending != 12 || got.OldestPendingAgeSeconds != 40 ||
		got.SizeBytes != 1<<20 || got.DroppedByKind["INFO"] != 7 || !got.ObservedAt.Equal(observed) || got.ReceivedAt.IsZero() {
		t.Fatalf("buffer status %+v", got)
	}

	// An older snapshot (a re-sent batch) never replaces a newer one.
	send(&models.BufferStatus{ObservedAt: observed.Add(-time.Second), Quarantined: 99})
	if got := buffer(); got.Quarantined != 3 {
		t.Fatalf("older snapshot replaced the newer one: %+v", got)
	}

	// An invalid snapshot is ignored; the statistics are stored all the same.
	send(&models.BufferStatus{ObservedAt: observed.Add(time.Second), Pending: -1})
	send(&models.BufferStatus{ObservedAt: observed.Add(time.Second), DroppedByKind: map[string]int64{"DEBUG": 1}})
	if got := buffer(); got.Quarantined != 3 || count(t, db, "SELECT COUNT(*) FROM machine_stats WHERE host_id=$1", h.ID) != 5 {
		t.Fatalf("invalid snapshot handling: %+v", got)
	}

	send(&models.BufferStatus{ObservedAt: observed.Add(2 * time.Second), DeliveryPaused: true})
	if got := buffer(); got.Quarantined != 0 || !got.DeliveryPaused || len(got.DroppedByKind) != 0 {
		t.Fatalf("newer snapshot not stored: %+v", got)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM hosts WHERE id=$1", h.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM host_buffer_status") != 0 {
		t.Fatal("buffer status outlived its host")
	}
}
