//go:build integration

package integration_test

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"testing"

	"monitor-central/internal/alertmetric"
	"monitor-central/internal/repositories"
)

// TestEveryAlertMetricIsEvaluatedFromTheDatabase fixes the value each alert
// metric computes, and which rules are evaluated at all (characterisation test
// for ADR-0008: it must pass before and after the metrics become strategies).
func TestEveryAlertMetricIsEvaluatedFromTheDatabase(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	fresh, stale := host(t, db, "fresh"), host(t, db, "stale")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO machine_stats(host_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at) VALUES
 ($1,42,2048,8192,40,100,CURRENT_TIMESTAMP-INTERVAL '1 minute'),
 ($1,99,8192,8192,99,100,CURRENT_TIMESTAMP-INTERVAL '3 minutes'),
 ($2,77,1,2,1,2,CURRENT_TIMESTAMP-INTERVAL '10 minutes')`, fresh.ID, stale.ID)
	exec(`INSERT INTO error_logs(host_id,app_name,environment,level,message,created_at) VALUES
 ($1,'app','test','ERROR','e1',CURRENT_TIMESTAMP-INTERVAL '10 seconds'),
 ($1,'app','test','ERROR','e2',CURRENT_TIMESTAMP-INTERVAL '20 seconds'),
 ($1,'app','test','CRITICAL','c1',CURRENT_TIMESTAMP-INTERVAL '30 seconds'),
 ($1,'app','test','INFO','i1',CURRENT_TIMESTAMP-INTERVAL '40 seconds'),
 ($1,'app','test','ERROR','old',CURRENT_TIMESTAMP-INTERVAL '2 minutes'),
 ($1,'app','test','PANIC','older',CURRENT_TIMESTAMP-INTERVAL '6 minutes')`, fresh.ID)
	exec(`UPDATE hosts SET last_seen_at=CURRENT_TIMESTAMP-INTERVAL '10 minutes' WHERE id=$1`, fresh.ID)
	// Buffer reports (ADR-0010): only the one received in the last 5 minutes counts.
	exec(`INSERT INTO host_buffer_status(host_id,observed_at,pending,quarantined,oldest_pending_age_seconds,delivery_paused,size_bytes,received_at) VALUES
 ($1,CURRENT_TIMESTAMP,0,3,0,FALSE,4096,CURRENT_TIMESTAMP-INTERVAL '1 minute'),
 ($2,CURRENT_TIMESTAMP,0,8,0,FALSE,4096,CURRENT_TIMESTAMP-INTERVAL '10 minutes')`, fresh.ID, stale.ID)
	exec(`INSERT INTO alerts(name,host_id,metric,condition,threshold) VALUES('stale quarantine',$1,'buffer_quarantined','gt',1)`, stale.ID)
	rules := map[string]int64{}
	for _, metric := range []string{"cpu", "ram", "disk", "error_rate", "error_level", "host_offline", "buffer_quarantined"} {
		var id int64
		if err := db.QueryRowContext(ctx, `INSERT INTO alerts(name,host_id,metric,condition,threshold) VALUES($1,$2,$1,'gt',1) RETURNING id`, metric, fresh.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		rules[metric] = id
	}
	exec(`INSERT INTO alerts(name,host_id,metric,condition,threshold) VALUES('stale cpu',$1,'cpu','gt',1)`, stale.ID)
	var disabled int64
	if err := db.QueryRowContext(ctx, `INSERT INTO alerts(name,host_id,metric,condition,threshold,enabled) VALUES('disabled',$1,'cpu','gt',1,FALSE) RETURNING id`, fresh.ID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}

	evaluations, err := repositories.NewAlertsRepository(db).ListMetricEvaluations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]float64{}
	for _, e := range evaluations {
		if e.HostID == stale.ID && (e.Metric == "cpu" || e.Metric == "buffer_quarantined") {
			t.Fatal("statistics and buffer reports older than 5 minutes must not be evaluated")
		}
		if e.HostID == fresh.ID {
			got[e.AlertID] = e.CurrentValue
		}
	}
	want := map[string]float64{
		"cpu": 42, "ram": 25, "disk": 40, // latest statistics only
		"error_rate":         3,  // ERROR, CRITICAL and PANIC in the last minute
		"error_level":        4,  // highest level in the last 5 minutes (CRITICAL)
		"host_offline":       10, // minutes since the last contact
		"buffer_quarantined": 3,  // the agent's last buffer report
	}
	for metric, value := range want {
		v, ok := got[rules[metric]]
		if !ok || math.Abs(v-value) > 0.1 {
			t.Errorf("%s: got %v (evaluated=%v), want %v", metric, v, ok, value)
		}
	}
	if _, ok := got[disabled]; ok {
		t.Error("a disabled rule must not be evaluated")
	}
}

// TestRegisteredMetricsMatchTheDatabaseConstraint: a metric added to the
// registry without the migration that allows its name fails here, not in
// production (ADR-0008).
func TestRegisteredMetricsMatchTheDatabaseConstraint(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	var definition string
	if err := db.QueryRowContext(context.Background(), `SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
 JOIN pg_class t ON t.oid = c.conrelid WHERE t.relname = 'alerts' AND c.contype = 'c'
 AND pg_get_constraintdef(c.oid) LIKE '%metric%'`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	allowed := regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(definition, -1)
	var names []string
	for _, m := range allowed {
		names = append(names, m[1])
	}
	sort.Strings(names)
	if got, want := strings.Join(names, ","), strings.Join(alertmetric.Names(), ","); got != want {
		t.Fatalf("alerts.metric CHECK allows %s, the registry has %s", got, want)
	}
}
