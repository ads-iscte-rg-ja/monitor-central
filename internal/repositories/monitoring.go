package repositories

import (
	"context"
	"database/sql"
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
func (r *MonitoringRepository) CreateMachineStats(ctx context.Context, hostID int64, stats []*models.MachineStat) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range stats {
		id, fresh, err := claimTelemetry(ctx, tx, hostID, "stat", "machine_stats", s.SourceID)
		if err != nil {
			return err
		}
		s.ID = id
		if fresh {
			_, err = tx.ExecContext(ctx, `INSERT INTO machine_stats(id,host_id,source_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.ID, hostID, s.SourceID, s.CPUPercent, s.RAMUsedMB, s.RAMTotalMB, s.DiskUsedGB, s.DiskTotalGB, s.CollectedAt)
			if err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hosts SET last_seen_at=CURRENT_TIMESTAMP,
 status=CASE WHEN health_endpoint='' THEN 'online' ELSE status END,
 unavailable_since=CASE WHEN health_endpoint='' THEN NULL ELSE unavailable_since END WHERE id=$1`, hostID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *MonitoringRepository) CreateErrorLogs(ctx context.Context, hostID int64, logs []*models.ErrorLog) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO error_logs(id,host_id,source_id,app_name,environment,level,message,stack_trace,metadata,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`)
	if err != nil {
		return 0, err
	}
	defer statement.Close()
	inserted := 0
	for _, entry := range logs {
		id, fresh, err := claimTelemetry(ctx, tx, hostID, "error", "error_logs", entry.SourceID)
		if err != nil {
			return 0, err
		}
		if !fresh {
			continue
		}
		var metadata any
		if len(entry.Metadata) > 0 {
			metadata = string(entry.Metadata)
		}
		result, err := statement.ExecContext(ctx, id, hostID, entry.SourceID, entry.AppName, entry.Environment, entry.Level, entry.Message, entry.StackTrace, metadata, entry.CreatedAt)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		inserted += int(count)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE hosts SET last_seen_at=CURRENT_TIMESTAMP,
 status=CASE WHEN health_endpoint='' THEN 'online' ELSE status END,
 unavailable_since=CASE WHEN health_endpoint='' THEN NULL ELSE unavailable_since END WHERE id=$1`, hostID); err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}

func claimTelemetry(ctx context.Context, tx *sql.Tx, hostID int64, kind, table string, sourceID *int64) (int64, bool, error) {
	var id int64
	if sourceID == nil {
		err := tx.QueryRowContext(ctx, "SELECT nextval(pg_get_serial_sequence($1,'id'))", table).Scan(&id)
		return id, true, err
	}
	err := tx.QueryRowContext(ctx, `INSERT INTO telemetry_receipts(host_id,kind,source_id,record_id)
 VALUES($1,$2,$3,nextval(pg_get_serial_sequence($4,'id')))
 ON CONFLICT(host_id,kind,source_id) DO NOTHING RETURNING record_id`, hostID, kind, *sourceID, table).Scan(&id)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, "SELECT record_id FROM telemetry_receipts WHERE host_id=$1 AND kind=$2 AND source_id=$3", hostID, kind, *sourceID).Scan(&id)
		return id, false, err
	}
	return id, true, err
}
