// Package services is the business layer of the central (layered style,
// ADR-0019): ingestion, alerts, host health, dashboard and authentication
// rules, and the background jobs' work.
//
// It declares the repository interfaces it needs and depends on them, not on
// implementations. It must not use handlers, grpcapi, server, templating or
// database (depguard rule central-services): it neither knows the transport
// nor runs SQL.
package services
