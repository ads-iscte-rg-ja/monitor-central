// Package repositories is the data access layer of the central (layered
// style, ADR-0019): the only package with SQL for PostgreSQL (ADR-0018).
// Batch writes (ADR-0005), receipts (ADR-0020) and the bounded dashboard
// queries (ADR-0017) live here, hidden behind the services' interfaces.
//
// It must not use services, handlers or server (depguard rule
// central-repositories): dependencies only point down.
package repositories
