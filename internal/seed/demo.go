package seed

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"monitor-central/internal/database"
	"monitor-central/internal/utils/apikey"
)

// Demo is idempotent and identifies its own rows by the explicit demo marker.
func Demo(ctx context.Context, db *database.DB, hasher *apikey.Hasher) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(743819113)"); err != nil {
		return err
	}
	for i, name := range []string{"Demo web server", "Demo database server"} {
		hostname := []string{"web.demo.local", "database.demo.local"}[i]
		var id int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM hosts WHERE demo=TRUE AND hostname=$1 ORDER BY id LIMIT 1", hostname).Scan(&id)
		if err != nil {
			if err != sql.ErrNoRows {
				return err
			}
			key := make([]byte, 32)
			if _, err = rand.Read(key); err != nil {
				return err
			}
			rawKey := hex.EncodeToString(key)
			err = tx.QueryRowContext(ctx, `INSERT INTO hosts(name,hostname,api_key_digest,api_key_masked,status,last_seen_at,demo) VALUES($1,$2,$3,$4,'online',CURRENT_TIMESTAMP,TRUE) RETURNING id`, name, hostname, hasher.Digest(rawKey), apikey.Mask(rawKey)).Scan(&id)
			if err != nil {
				return err
			}
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE hosts SET status='online',last_seen_at=CURRENT_TIMESTAMP WHERE id=$1", id); err != nil {
				return err
			}
		}
		cpu := []float64{42, 95}[i]
		if _, err = tx.ExecContext(ctx, `INSERT INTO machine_stats(host_id,source_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at) VALUES($1,1,$2,4096,8192,120,500,CURRENT_TIMESTAMP)
 ON CONFLICT(host_id,source_id) DO UPDATE SET cpu_percent=EXCLUDED.cpu_percent,collected_at=EXCLUDED.collected_at`, id, cpu); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO error_logs(host_id,source_id,app_name,environment,level,message,created_at) VALUES($1,1,'demo-app','demonstration','ERROR','Simulated request timeout',CURRENT_TIMESTAMP)
 ON CONFLICT(host_id,source_id) DO UPDATE SET created_at=EXCLUDED.created_at`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO alerts(name,host_id,metric,condition,threshold,severity,notification_channels,enabled)
 SELECT 'Demo CPU rule',$1,'cpu','gt',80,'critical',ARRAY['dashboard'],TRUE WHERE NOT EXISTS(SELECT 1 FROM alerts WHERE host_id=$1 AND name='Demo CPU rule')`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
