//go:build integration

package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	databasesql "monitor-central/deployments/database"
	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
	"monitor-central/internal/utils/apikey"
)

func TestVersionOneUpgradePreservesKeysAndReceipts(t *testing.T) {
	db := isolatedDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, databasesql.InitSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO hosts(name,hostname,api_key) VALUES('old','old.local','existing-private-key');
 INSERT INTO machine_stats(host_id,source_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at) VALUES(1,4,25,1,2,1,2,NOW()-INTERVAL '10 days');
 INSERT INTO error_logs(host_id,source_id,app_name,environment,level,message) VALUES(1,9,'app','test','ERROR','preserved');
 INSERT INTO sessions(token,data,expiry) VALUES('legacy-key-flash','',NOW()+INTERVAL '1 day')`); err != nil {
		t.Fatal(err)
	}
	migrate(t, db)
	migrate(t, db)
	if count(t, db, "SELECT COUNT(*) FROM telemetry_receipts") != 2 {
		t.Fatal("receipt backfill failed")
	}
	if count(t, db, "SELECT COUNT(*) FROM sessions") != 0 {
		t.Fatal("legacy key-flash sessions retained")
	}
	if count(t, db, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='hosts' AND column_name='api_key'") != 0 {
		t.Fatal("raw credential column retained")
	}
	repo := repositories.NewMonitoringRepository(db, testHasher(t))
	if h, err := repo.GetHostByAPIKey(ctx, "existing-private-key"); err != nil || h.ID != 1 {
		t.Fatal("existing key stopped authenticating", err)
	}
	hostRepo := repositories.NewHostsRepository(db, testHasher(t))
	if err := hostRepo.UpdateHostAPIKey(ctx, 1, "new-private-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetHostByAPIKey(ctx, "existing-private-key"); err == nil {
		t.Fatal("rotated key still authenticates")
	}
	if _, err := repo.GetHostByAPIKey(ctx, "new-private-key"); err != nil {
		t.Fatal(err)
	}
	wrong, _ := apikey.New([]byte(strings.Repeat("b", 32)))
	if err := db.CheckAPIKeySecret(ctx, wrong); err == nil {
		t.Fatal("wrong secret accepted on startup")
	}
	if err := db.Migrate(ctx, wrong); err == nil {
		t.Fatal("wrong secret accepted by migration")
	}
}

func TestConcurrentTelemetryReceipts(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "concurrent-ingest")
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.IngestStats(ctx, h.ID, &models.MachineStatsRequest{ID: 1, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2})
			errors <- err
			_, _, err = s.IngestErrors(ctx, h.ID, &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{{ID: 1, AppName: "app", Environment: "test", Level: "ERROR", Message: "retry"}}})
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count(t, db, "SELECT COUNT(*) FROM machine_stats") != 1 || count(t, db, "SELECT COUNT(*) FROM error_logs") != 1 || count(t, db, "SELECT COUNT(*) FROM telemetry_receipts") != 2 {
		t.Fatal("concurrent retries duplicated records")
	}
}

func TestRetentionIsBoundedPreservesPendingAndRetryReceipts(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "retention")
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	stat := &models.MachineStatsRequest{ID: 1, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2, CollectedAt: old}
	first, err := s.IngestStats(ctx, h.ID, stat)
	if err != nil {
		t.Fatal(err)
	}
	input := &models.ErrorLogsRequest{Errors: []models.ErrorLogInput{{ID: 1, AppName: "app", Environment: "test", Level: "ERROR", Message: "old failure", CreatedAt: old}, {ID: 2, AppName: "app", Environment: "test", Level: "ERROR", Message: "another old failure", CreatedAt: old}}}
	if _, _, err := s.IngestErrors(ctx, h.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alerts(name,host_id,metric,condition,threshold) VALUES('rule',$1,'cpu','gt',80)`, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alert_events(alert_id,host_id,triggered_value,status,fired_at,resolved_at) VALUES(1,$1,90,'resolved',NOW()-INTERVAL '10 days',NOW()-INTERVAL '9 days'),(1,$1,90,'resolved',NOW()-INTERVAL '10 days',NOW()-INTERVAL '9 days'),(1,$1,90,'fired',NOW()-INTERVAL '10 days',NULL)`, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO email_outbox(event_id,recipient,subject,text_body,html_body,dedupe_key,created_at,sent_at) VALUES(1,'pending@example.com','pending','text','html','pending',NOW()-INTERVAL '10 days',NULL),(2,'sent@example.com','sent','text','html','sent',NOW()-INTERVAL '10 days',NOW()-INTERVAL '9 days')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users(email,password_hash) VALUES('audit@example.com','test-only'); INSERT INTO audit_log(user_id,action,timestamp) VALUES(1,'login',NOW()-INTERVAL '10 days'),(1,'login',NOW())`); err != nil {
		t.Fatal(err)
	}
	r := repositories.NewRetentionRepository(db)
	if counts, err := r.Cleanup(ctx, config.RetentionConfig{BatchSize: 1}, time.Now()); err != nil || len(counts) != 0 {
		t.Fatal("disabled cleanup removed data", err)
	}
	policy := config.RetentionConfig{StatsDays: 1, ErrorDays: 1, ResolvedEventDays: 1, SentEmailDays: 1, AuditDays: 1, BatchSize: 1}
	counts, err := r.Cleanup(ctx, policy, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if counts["machine_stats"] != 1 || counts["error_logs"] != 1 || counts["alert_events"] != 1 || counts["email_outbox"] != 1 {
		t.Fatalf("unexpected cleanup counts %+v", counts)
	}
	if counts["audit_log"] != 1 || count(t, db, "SELECT COUNT(*) FROM audit_log") != 1 {
		t.Fatal("audit retention failed")
	}
	if count(t, db, "SELECT COUNT(*) FROM email_outbox WHERE sent_at IS NULL") != 1 || count(t, db, "SELECT COUNT(*) FROM alert_events WHERE status='fired'") != 1 || count(t, db, "SELECT COUNT(*) FROM alert_events WHERE id=1") != 1 {
		t.Fatal("pending delivery or unresolved incident lost")
	}
	if _, err := r.Cleanup(ctx, policy, time.Now()); err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.IngestStats(ctx, h.ID, stat)
	if err != nil || duplicate.ID != first.ID || count(t, db, "SELECT COUNT(*) FROM machine_stats") != 0 {
		t.Fatal("pruned statistics reinserted", err)
	}
	accepted, inserted, err := s.IngestErrors(ctx, h.ID, input)
	if err != nil || accepted != 2 || inserted != 0 || count(t, db, "SELECT COUNT(*) FROM error_logs") != 0 {
		t.Fatal("pruned errors reinserted", err)
	}
}
