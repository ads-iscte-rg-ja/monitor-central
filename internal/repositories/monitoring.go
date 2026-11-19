package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
	"monitor-central/internal/utils/apikey"
)

type MonitoringRepository struct {
	db     *database.DB
	hasher *apikey.Hasher
}

func NewMonitoringRepository(db *database.DB, hasher *apikey.Hasher) *MonitoringRepository {
	return &MonitoringRepository{db: db, hasher: hasher}
}

func (r *MonitoringRepository) GetHostByAPIKey(ctx context.Context, key string) (*models.Host, error) {
	var host models.Host
	err := r.db.Get(ctx, &host, `SELECT id,name,hostname,status,last_seen_at,created_at FROM hosts WHERE api_key_digest=$1`, r.hasher.Digest(key))
	if err == sql.ErrNoRows {
		return nil, models.ErrHostNotFound
	}
	if err != nil {
		return nil, err
	}
	return &host, nil
}

// CreateMachineStats stores a batch in one transaction with a fixed number of
// statements, whatever the batch size (ADR-0005): receipts, rows, host update.
func (r *MonitoringRepository) CreateMachineStats(ctx context.Context, hostID int64, stats []*models.MachineStat) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sources := make([]*int64, len(stats))
	for i, s := range stats {
		sources[i] = s.SourceID
	}
	claims, err := claimTelemetry(ctx, tx, hostID, "stat", "machine_stats", sources)
	if err != nil {
		return err
	}
	var ids, sourceIDs []int64
	var cpu, ramUsed, ramTotal, diskUsed, diskTotal []float64
	var collected []string
	for i, s := range stats {
		s.ID = claims[i].id
		if !claims[i].fresh {
			continue
		}
		ids = append(ids, s.ID)
		sourceIDs = append(sourceIDs, nullableSource(s.SourceID))
		cpu = append(cpu, s.CPUPercent)
		ramUsed = append(ramUsed, s.RAMUsedMB)
		ramTotal = append(ramTotal, s.RAMTotalMB)
		diskUsed = append(diskUsed, s.DiskUsedGB)
		diskTotal = append(diskTotal, s.DiskTotalGB)
		collected = append(collected, s.CollectedAt.UTC().Format(time.RFC3339Nano))
	}
	if len(ids) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO machine_stats(id,host_id,source_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at)
 SELECT id, $1, NULLIF(source_id,0), cpu, ram_used, ram_total, disk_used, disk_total, collected_at::timestamptz
 FROM unnest($2::bigint[], $3::bigint[], $4::float8[], $5::float8[], $6::float8[], $7::float8[], $8::float8[], $9::text[])
   AS t(id, source_id, cpu, ram_used, ram_total, disk_used, disk_total, collected_at)`,
			hostID, pq.Array(ids), pq.Array(sourceIDs), pq.Array(cpu), pq.Array(ramUsed), pq.Array(ramTotal),
			pq.Array(diskUsed), pq.Array(diskTotal), pq.Array(collected))
		if err != nil {
			return err
		}
	}
	if err := touchHost(ctx, tx, hostID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateErrorLogs stores a batch like CreateMachineStats and returns how many
// rows were new (the rest were re-sent duplicates).
func (r *MonitoringRepository) CreateErrorLogs(ctx context.Context, hostID int64, logs []*models.ErrorLog) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	sources := make([]*int64, len(logs))
	for i, entry := range logs {
		sources[i] = entry.SourceID
	}
	claims, err := claimTelemetry(ctx, tx, hostID, "error", "error_logs", sources)
	if err != nil {
		return 0, err
	}
	var ids, sourceIDs []int64
	var apps, envs, levels, messages, created []string
	var stacks, metadata []sql.NullString
	for i, entry := range logs {
		if !claims[i].fresh {
			continue
		}
		ids = append(ids, claims[i].id)
		sourceIDs = append(sourceIDs, nullableSource(entry.SourceID))
		apps = append(apps, entry.AppName)
		envs = append(envs, entry.Environment)
		levels = append(levels, entry.Level)
		messages = append(messages, entry.Message)
		stack := sql.NullString{}
		if entry.StackTrace != nil {
			stack = sql.NullString{String: *entry.StackTrace, Valid: true}
		}
		stacks = append(stacks, stack)
		metadata = append(metadata, sql.NullString{String: string(entry.Metadata), Valid: len(entry.Metadata) > 0})
		created = append(created, entry.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	inserted := 0
	if len(ids) > 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO error_logs(id,host_id,source_id,app_name,environment,level,message,stack_trace,metadata,created_at)
 SELECT id, $1, NULLIF(source_id,0), app, env, level, message, stack, metadata::jsonb, created_at::timestamptz
 FROM unnest($2::bigint[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[])
   AS t(id, source_id, app, env, level, message, stack, metadata, created_at)`,
			hostID, pq.Array(ids), pq.Array(sourceIDs), pq.Array(apps), pq.Array(envs), pq.Array(levels),
			pq.Array(messages), pq.Array(stacks), pq.Array(metadata), pq.Array(created))
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		inserted = int(count)
	}
	if err := touchHost(ctx, tx, hostID); err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}

func touchHost(ctx context.Context, tx *sql.Tx, hostID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE hosts SET last_seen_at=CURRENT_TIMESTAMP,
 status=CASE WHEN health_endpoint='' THEN 'online' ELSE status END,
 unavailable_since=CASE WHEN health_endpoint='' THEN NULL ELSE unavailable_since END WHERE id=$1`, hostID)
	return err
}

// nullableSource encodes a missing source ID as 0, which the INSERT turns back
// into NULL (source IDs are always positive).
func nullableSource(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

type telemetryClaim struct {
	id    int64
	fresh bool
}

// claimTelemetry reserves a central ID for every record of a batch, in the
// batch's order, with one statement (ADR-0005). A record whose source ID was
// already received keeps its original ID and is not fresh, which makes a
// re-sent batch idempotent. Records without a source ID always get a new ID.
func claimTelemetry(ctx context.Context, tx *sql.Tx, hostID int64, kind, table string, sources []*int64) ([]telemetryClaim, error) {
	claims := make([]telemetryClaim, len(sources))
	position := map[int64]int{}
	var withSource []int64
	var anonymous []int
	for i, source := range sources {
		if source == nil {
			anonymous = append(anonymous, i)
			continue
		}
		withSource = append(withSource, *source)
		position[*source] = i
	}
	if err := claimSources(ctx, tx, hostID, kind, table, withSource, position, claims); err != nil {
		return nil, err
	}
	if err := allocateIDs(ctx, tx, table, anonymous, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// claimSources inserts the receipts of a batch. RETURNING only reports the
// rows this statement inserted; the others were received before, and a second
// statement reads their IDs (it also sees receipts committed by a concurrent
// transaction after the INSERT started).
func claimSources(ctx context.Context, tx *sql.Tx, hostID int64, kind, table string, sources []int64, position map[int64]int, claims []telemetryClaim) error {
	if len(sources) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `INSERT INTO telemetry_receipts(host_id,kind,source_id,record_id)
 SELECT $1, $2, source_id, nextval(pg_get_serial_sequence($3,'id'))
 FROM unnest($4::bigint[]) AS t(source_id)
 ON CONFLICT(host_id,kind,source_id) DO NOTHING RETURNING source_id, record_id`,
		hostID, kind, table, pq.Array(sources))
	if err != nil {
		return err
	}
	fresh, err := scanClaims(rows, position, claims, true)
	if err != nil {
		return err
	}
	if fresh == len(sources) {
		return nil
	}
	var repeated []int64
	for _, source := range sources {
		if !claims[position[source]].fresh {
			repeated = append(repeated, source)
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT source_id, record_id FROM telemetry_receipts WHERE host_id=$1 AND kind=$2 AND source_id = ANY($3::bigint[])`,
		hostID, kind, pq.Array(repeated))
	if err != nil {
		return err
	}
	found, err := scanClaims(rows, position, claims, false)
	if err != nil {
		return err
	}
	if found != len(repeated) {
		return fmt.Errorf("telemetry receipts: %d of %d repeated sources not found", len(repeated)-found, len(repeated))
	}
	return nil
}

// scanClaims reads (source_id, record_id) rows into the claims and returns how many it read.
func scanClaims(rows *sql.Rows, position map[int64]int, claims []telemetryClaim, fresh bool) (int, error) {
	defer rows.Close()
	n := 0
	for rows.Next() {
		var source, id int64
		if err := rows.Scan(&source, &id); err != nil {
			return 0, err
		}
		claims[position[source]] = telemetryClaim{id: id, fresh: fresh}
		n++
	}
	return n, rows.Err()
}

// allocateIDs gives new IDs to records without a source ID.
func allocateIDs(ctx context.Context, tx *sql.Tx, table string, indexes []int, claims []telemetryClaim) error {
	if len(indexes) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id')) FROM generate_series(1,$2)`, table, len(indexes))
	if err != nil {
		return err
	}
	defer rows.Close()
	for _, i := range indexes {
		if !rows.Next() {
			return fmt.Errorf("allocate telemetry IDs: sequence returned too few values")
		}
		if err := rows.Scan(&claims[i].id); err != nil {
			return err
		}
		claims[i].fresh = true
	}
	return rows.Err()
}
