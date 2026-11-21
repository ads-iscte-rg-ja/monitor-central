package database

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"fmt"
	"strings"

	databasesql "monitor-central/deployments/database"
	"monitor-central/internal/utils/apikey"
)

const SchemaVersion = 8

// Migrate initializes a database or upgrades legacy TEXT IDs atomically.
func (db *DB) Migrate(ctx context.Context, hasher *apikey.Hasher) error {
	if hasher == nil {
		return fmt.Errorf("API key hasher is required for migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(743819112)"); err != nil {
		return err
	}
	// The 30 s statement timeout of every connection (postgres.go) is for
	// requests; a migration that rebuilds an index or fills a table over a large
	// database runs longer (ADR-0017). The caller's context bounds it instead.
	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout = 0"); err != nil {
		return err
	}
	var migrationsExist bool
	if err = tx.QueryRowContext(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&migrationsExist); err != nil {
		return err
	}
	version := 0
	if migrationsExist {
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil {
			return err
		}
	}
	if version > SchemaVersion {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	if version == SchemaVersion {
		if err = checkAPIKeySecret(ctx, tx, hasher); err != nil {
			return err
		}
		return tx.Commit()
	}
	if version == 0 {
		var dataType string
		err = tx.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='users' AND column_name='id'`).Scan(&dataType)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		script := databasesql.InitSQL
		if dataType == "text" {
			script = strings.Replace(databasesql.LegacyTextToNumericSQL, "-- CREATE_NEW_SCHEMA", databasesql.InitSQL, 1)
		} else if dataType != "" && dataType != "bigint" {
			return fmt.Errorf("unsupported users.id type %q", dataType)
		}
		if _, err = tx.ExecContext(ctx, script); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}
	if version >= 2 {
		if err = checkAPIKeySecret(ctx, tx, hasher); err != nil {
			return err
		}
	}
	if version < 2 {
		if _, err = tx.ExecContext(ctx, databasesql.SecurityRetentionSQL); err != nil {
			return fmt.Errorf("migrate security schema: %w", err)
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,api_key FROM hosts")
		if err != nil {
			return err
		}
		type credential struct {
			id  int64
			key string
		}
		var credentials []credential
		for rows.Next() {
			var c credential
			if err = rows.Scan(&c.id, &c.key); err != nil {
				rows.Close()
				return err
			}
			credentials = append(credentials, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range credentials {
			if _, err = tx.ExecContext(ctx, "UPDATE hosts SET api_key_digest=$1,api_key_masked=$2 WHERE id=$3", hasher.Digest(c.key), apikey.Mask(c.key), c.id); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `ALTER TABLE hosts ALTER COLUMN api_key_digest SET NOT NULL;
 ALTER TABLE hosts ALTER COLUMN api_key_masked SET NOT NULL;
 ALTER TABLE hosts ADD CONSTRAINT hosts_api_key_digest_unique UNIQUE(api_key_digest);
 ALTER TABLE hosts DROP COLUMN api_key;
 INSERT INTO schema_migrations(version) VALUES(2)`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO api_key_settings(id,secret_fingerprint) VALUES(1,$1)", hasher.Fingerprint()); err != nil {
			return err
		}
	}
	if err = applyScripts(ctx, tx, version); err != nil {
		return err
	}
	return tx.Commit()
}

// applyScripts runs the plain-SQL migrations newer than version, in order.
func applyScripts(ctx context.Context, tx *sql.Tx, version int) error {
	for _, m := range []struct {
		version int
		name    string
		script  string
	}{
		{3, "host health", databasesql.HostHealthSQL},
		{4, "login attempts", databasesql.LoginAttemptsSQL},
		{5, "host buffer status", databasesql.HostBufferStatusSQL},
		{6, "buffer_quarantined alert metric", databasesql.AlertBufferQuarantinedSQL},
		{7, "host health schedule", databasesql.HostHealthScheduleSQL},
		{8, "dashboard queries over 30 days", databasesql.Dashboard30dSQL},
	} {
		if version >= m.version {
			continue
		}
		if _, err := tx.ExecContext(ctx, m.script); err != nil {
			return fmt.Errorf("migrate %s schema: %w", m.name, err)
		}
	}
	return nil
}

func (db *DB) CheckSchema(ctx context.Context) error {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("run go run ./cmd/migrate first: %w", err)
	}
	if version != SchemaVersion {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	return nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkAPIKeySecret(ctx context.Context, db rowQuerier, hasher *apikey.Hasher) error {
	var fingerprint string
	if err := db.QueryRowContext(ctx, "SELECT secret_fingerprint FROM api_key_settings WHERE id=1").Scan(&fingerprint); err != nil {
		return err
	}
	if !hmac.Equal([]byte(fingerprint), []byte(hasher.Fingerprint())) {
		return fmt.Errorf("HOST_API_KEY_SECRET does not match this database; restore the original secret")
	}
	return nil
}

func (db *DB) CheckAPIKeySecret(ctx context.Context, hasher *apikey.Hasher) error {
	return checkAPIKeySecret(ctx, db, hasher)
}
