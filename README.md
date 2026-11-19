# Monitor central

Central receives host measurements and application logs, stores them, and provides
the dashboard, host health checks, alert evaluation, and email notifications.

## Architecture

```text
Monitored host
  Application → monitor-go-sdk → SQLite buffer ← CPU/RAM/disk collection
                                     │
                               monitor-remote
                                     │ authenticated gRPC batches
                                     ▼
                               monitor-central ───→ PostgreSQL
                                     │
                    ┌────────────────┼──────────────────┐
                    ▼                ▼                  ▼
              Web dashboard    Alert evaluation   Host health probes
                                     │                  │
                              Email outbox       Application /health
                                     │
                                    SMTP
```

- **Applications** use the SDK to write logs to the local SQLite buffer.
- **Remote** initializes that buffer, collects resources, and delivers telemetry.
  It flushes the starting backlog newest first, in batches of up to 100. Records
  remain pending until central acknowledges them; invalid records are quarantined.
- **Central** receives telemetry over gRPC (contract and error codes in
  [monitor-api](https://github.com/ads-iscte-rg-ja/monitor-api)), authenticates each
  host with its API key, validates batches, and commits telemetry and
  deduplication receipts together. Persistent source IDs
  make retries safe.
- **Background jobs** check host availability, evaluate alerts, deliver queued
  email with retries, run retention cleanup, and prepare daily summaries. They
  run inside central using Go timers; there is no separate message broker.
- **Host availability** comes from an optional application health endpoint probed
  by central, or a configurable last-contact timeout. Measurement freshness is
  tracked independently of host availability.

Central follows a handler → service → repository structure: HTTP and rendering,
business rules, then SQL persistence. Alert events and their email-outbox entries
commit in one transaction; SMTP delivery happens separately and is at-least-once.

## Tech stack

| Area | Technology |
| --- | --- |
| Backend | Go 1.26+, standard `net/http` and Chi (dashboard), gRPC (telemetry ingestion) |
| Central storage | PostgreSQL 16, `database/sql`, `lib/pq`, embedded SQL migrations |
| Frontend | Go `html/template`, vanilla JavaScript, SVG charts, Tailwind CSS from CDN |
| Authentication | SCS PostgreSQL-backed sessions, bcrypt passwords, HMAC-SHA256 host-key digests |
| Email | SMTP through `go-mail`, PostgreSQL-backed outbox |
| Remote collection/storage | `gopsutil`, SQLite through `mattn/go-sqlite3` |
| Development tooling | Go workspace, Make, Docker Compose |

Central serves the frontend itself; no Node or frontend build is required.

## Setup and run

Requirements: **Go 1.26+**, **Make**, **Docker with Compose**, and a reachable
**SMTP server**. Run the following commands from `monitor-central/`.

### 1. Configure the environment

```sh
cp .env.example .env
chmod 600 .env
```

Edit `.env` and set:

- `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`.
- `ADMIN_EMAIL`, `ADMIN_PASSWORD` (12–72 bytes), `ADMIN_NAME`.
- Optionally, `ADMIN_NOTIFICATION_EMAIL` for alert emails.

Keep the example database address (`localhost:5433`) for host-local commands.
Compose supplies its internal database address automatically. The SMTP server
must be reachable from the backend container; `localhost` there means that
container. Compose does not include an SMTP server.

Generate the host-key secret once:

```sh
make key-secret
```

This fills `HOST_API_KEY_SECRET` in `.env`. Preserve it with database backups;
an existing database requires the same secret.

### 2. Start central

```sh
make docker/up/build
make docker/seed-admin
```

Compose starts PostgreSQL, runs migrations, then starts the backend. Administrator
seeding is explicit; rerunning it updates that account and revokes its sessions.

Open **http://localhost:3333** and sign in with the configured administrator.
The dashboard, gRPC ingestion, and database ports are published on loopback only.
`PORT` changes the dashboard's host port and `GRPC_PORT` the ingestion port
(default `3334`); PostgreSQL uses `5433`. Agents outside the host reach ingestion
through a TLS reverse proxy that supports HTTP/2.

### 3. Register a host

On `/hosts`, enter its name and hostname/IP, then copy its API key into
[remote's configuration](../monitor-remote/README.md).

- With a **health endpoint**, central checks it every 30 seconds with a five-second
  timeout. A 2xx response means online; redirects are not followed.
- Without an endpoint, set **Offline after** to allow the expected reporting gap.
  The default is five minutes; the range is one minute to 24 hours.

Set an alert recipient on `/profile` and configure rules on `/alerts`.

## Metrics and logs

Central exposes Prometheus metrics on a separate, non-public listener
(`METRICS_ADDR`, default `127.0.0.1:9464`; Compose publishes it on loopback):
ingestion latency and status codes, records inserted/duplicated/rejected,
ingestion lag, dashboard request latency, background job duration and
failures, outbox backlog, and database pool usage. Logs are structured JSON
(`LOG_FORMAT`, `LOG_LEVEL`), with the request ID for HTTP and the host ID for
failed gRPC ingestion calls.

For load tests and demos, start Prometheus and Grafana as well:

```sh
docker compose -f deployments/compose.yaml --profile observability up -d
```

Prometheus is then on `http://localhost:9090` and Grafana on
`http://localhost:3000`, both on loopback only.

## Useful commands

| Command | Purpose |
| --- | --- |
| `make docker/status` | Show container status |
| `make docker/logs` | Follow backend and migration logs |
| `make docker/down` | Stop the stack, preserving database volumes |
| `make help` | List all commands |

To run the backend directly with Go instead of the backend container, use the
same `.env` and generated secret, then:

```sh
make docker/db/up
make db/migrate
make seed/admin
make backend/run
```

## Configuration and upgrades

- All settings are listed in [`.env.example`](.env.example). For HTTPS deployments,
  use `APP_ENV=production` and `COOKIE_SECURE=true`.
- Ingestion is bounded by `INGEST_MAX_INFLIGHT` (default 60% of
  `POSTGRES_MAX_OPEN_CONNS`). Over the limit, agents get `RESOURCE_EXHAUSTED`
  with a 1–10 s retry delay and resend later from their buffer, so the
  dashboard keeps database connections during a recovery storm.
- Central retention is disabled by default. Set the `RETENTION_*_DAYS` values to
  enable it; `CLEANUP_INTERVAL` and `CLEANUP_BATCH_SIZE` control scheduled cleanup.
- Back up PostgreSQL and stop the backend before upgrading an existing database:

  ```sh
  make docker/stop/backend
  make docker/migrate
  make docker/up/build
  ```

See [database setup and migrations](deployments/database/README.md) for details.
The current schema version is **3**. Keep central and remote on compatible
versions of the `monitor-api` contract, and preserve each host's SQLite buffer
across restarts.

Recent health, buffering, and delivery changes await executable verification.
See the [implementation checklist](../IMPLEMENTATION_CHECKLIST.md) for status.
