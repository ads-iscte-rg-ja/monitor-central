package repositories

import (
	"context"
	"database/sql"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
	"monitor-central/internal/utils/apikey"
)

type HostsRepository struct {
	db     *database.DB
	hasher *apikey.Hasher
}

func NewHostsRepository(
	db *database.DB,
	hasher *apikey.Hasher,
) *HostsRepository {
	return &HostsRepository{db: db, hasher: hasher}
}

func (r *HostsRepository) ListHosts(
	ctx context.Context,
) ([]models.HostListItem, error) {
	hosts := []models.HostListItem{}

	err := r.db.Select(
		ctx,
		&hosts,
		`SELECT
			h.id,
			h.name,
			h.hostname,
			h.api_key_masked,
			h.status,
			h.last_seen_at,
			h.created_at,
			h.health_endpoint,
			h.contact_timeout_seconds,
			h.health_checked_at,
			h.health_check_ok,
			latest.cpu_percent::DOUBLE PRECISION
				AS cpu_percent,
			CASE
				WHEN latest.ram_total_mb > 0
				THEN (
					latest.ram_used_mb /
					latest.ram_total_mb
				) * 100
			END::DOUBLE PRECISION AS ram_percent,
			CASE
				WHEN latest.disk_total_gb > 0
				THEN (
					latest.disk_used_gb /
					latest.disk_total_gb
				) * 100
			END::DOUBLE PRECISION AS disk_percent
		 FROM hosts h
		 LEFT JOIN LATERAL (
			SELECT
				cpu_percent,
				ram_used_mb,
				ram_total_mb,
				disk_used_gb,
				disk_total_gb
			FROM machine_stats
			WHERE host_id = h.id
			ORDER BY collected_at DESC
			LIMIT 1
		 ) AS latest ON TRUE
		 ORDER BY h.name, h.id`,
	)
	if err != nil {
		return nil, err
	}

	return hosts, nil
}

func (r *HostsRepository) CreateHost(
	ctx context.Context,
	host *models.Host,
) error {
	err := r.db.QueryRowContext(
		ctx,
		`INSERT INTO hosts (
			name,
			hostname,
			api_key_digest,
			api_key_masked,
			status,
			health_endpoint,
			contact_timeout_seconds
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id,created_at`,
		host.Name,
		host.Hostname,
		r.hasher.Digest(host.APIKey),
		apikey.Mask(host.APIKey),
		host.Status,
		host.HealthEndpoint,
		host.ContactTimeoutSeconds,
	).Scan(&host.ID, &host.CreatedAt)

	return err
}

func (r *HostsRepository) DeleteHost(
	ctx context.Context,
	hostID int64,
) error {
	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM hosts
		 WHERE id = $1`,
		hostID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrHostNotFound
	}

	return nil
}

func (r *HostsRepository) UpdateHost(
	ctx context.Context,
	host *models.Host,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldEndpoint string
	if err := tx.QueryRowContext(ctx, "SELECT health_endpoint FROM hosts WHERE id=$1 FOR UPDATE", host.ID).Scan(&oldEndpoint); err == sql.ErrNoRows {
		return models.ErrHostNotFound
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE hosts
		 SET name = $1,
		     hostname = $2,
		     health_endpoint = $3,
		     contact_timeout_seconds = $4,
		     status = CASE WHEN health_endpoint<>$3 THEN 'unknown' ELSE status END,
		     health_checked_at = CASE WHEN health_endpoint<>$3 THEN NULL ELSE health_checked_at END,
		     health_check_ok = CASE WHEN health_endpoint<>$3 THEN NULL ELSE health_check_ok END,
		     unavailable_since = CASE WHEN health_endpoint<>$3 THEN NULL ELSE unavailable_since END
		 WHERE id = $5`,
		host.Name,
		host.Hostname,
		host.HealthEndpoint,
		host.ContactTimeoutSeconds,
		host.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrHostNotFound
	}
	if (oldEndpoint == "") != (host.HealthEndpoint == "") {
		if _, err := tx.ExecContext(ctx, `UPDATE alerts SET threshold=CASE WHEN $2='' THEN $3::DOUBLE PRECISION/60 ELSE 5 END WHERE host_id=$1 AND system_rule=TRUE AND metric='host_offline'`, host.ID, host.HealthEndpoint, host.ContactTimeoutSeconds); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *HostsRepository) UpdateHostAPIKey(
	ctx context.Context,
	hostID int64,
	apiKey string,
) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE hosts
		 SET api_key_digest = $1, api_key_masked = $2
		 WHERE id = $3`,
		r.hasher.Digest(apiKey),
		apikey.Mask(apiKey),
		hostID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return models.ErrHostNotFound
	}

	return nil
}
