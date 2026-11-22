# PostgreSQL initialization and migrations

```text
database/
  init.sql                             Immutable version-1 baseline
  embed.go                             Embeds these same SQL files for Go tools
  migrations/
    002_security_retention.sql          Version-1 to version-2 structural changes
    003_host_health.sql                 Version-2 to version-3 host health settings
    004_login_attempts.sql              Version 4: login limiter shared by instances (ADR-0007)
    005_host_buffer_status.sql          Version 5: last buffer report per host (ADR-0010)
    006_alert_buffer_quarantined.sql    Version 6: buffer_quarantined alert metric (ADR-0010)
    legacy_text_to_numeric.sql         Special pre-versioned TEXT-ID conversion
```

## New databases

`deployments/compose.yaml` mounts `database/init.sql` into PostgreSQL's
`/docker-entrypoint-initdb.d/001_init.sql`. The official image executes it only
when the data directory is empty. It creates schema version 1.

The Compose `migrate` service waits for healthy PostgreSQL, then runs
`cmd/migrate` using the database settings and `HOST_API_KEY_SECRET` from the
configured environment. The backend depends on successful migration completion.
This upgrades to the current version and binds the database to the configured
HMAC secret. Run `make docker/seed-admin` explicitly after initial startup.

For a host-local Go process, start only PostgreSQL with `make docker/db/up`, run
`make db/migrate` and `make seed/admin`, then `make backend/run`. The app rejects
an outdated schema in either mode.

The migration command can also initialize an empty non-Compose database using
the embedded `init.sql` before upgrading it through the same path.

## Existing databases

Back up the database and stop the application before an upgrade. For Compose use
`make docker/stop/backend`, `make docker/migrate`, and `make docker/up/build`.
For local Go processes use `make db/migrate`, then start the upgraded app.
Restarting PostgreSQL or editing `init.sql` does not upgrade an existing volume.

The current schema version is 8. Older databases are upgraded through every
missing migration, in order, by the migration command before starting the
updated backend. Keep the existing database configuration, persistent volume, and
`HOST_API_KEY_SECRET`. The upgrade adds default contact-based health settings
and preserves host credentials, telemetry, and deduplication receipts. The
integration tests run the upgrade from version 2 to the current version.

## Migration ownership

- Keep `init.sql` as the baseline. Add subsequent changes as numbered migrations
  (the next version is `007_...sql`) rather than rewriting deployed history.
- `internal/database/migrate.go` explicitly orders the migration steps, checks
  schema versions, and executes them under a transaction and advisory lock.
  Add each new migration to the embedded scripts and the runner when introduced.
- The legacy script runs only when the old TEXT-ID schema is detected. It is not
  a normal numbered migration and is not automatically run on fresh databases.
- Version 2 combines SQL changes with a Go data-conversion step that derives
  HMAC digests using the environment secret, removes raw keys, and records the
  completed version. All steps commit or roll back together.
- Version 3 adds optional central-probed health URLs, per-host contact timeouts,
  and the latest check/failure state. A version-2 upgrade verifies the existing
  HMAC secret and does not rerun credential conversion.
- Version 4 adds the login limiter windows; version 5 the last buffer report of
  each host; version 6 allows the `buffer_quarantined` alert metric. A new alert
  metric is a file in `internal/alertmetric` plus a migration like version 6; an
  integration test compares the registry with the `CHECK` constraint.
- `embed.go` keeps the exact deployment files inside compiled migration tools;
  no runtime SQL directory or duplicate schema copy is needed.
- PostgreSQL initialization runs only `init.sql`. The migrations directory is
  deliberately not mounted as an entrypoint script directory.
