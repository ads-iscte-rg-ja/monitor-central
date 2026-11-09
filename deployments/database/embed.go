// Package databasesql embeds the same PostgreSQL scripts used by deployments.
// init.sql is the immutable version-1 baseline; later changes are migrations.
package databasesql

import _ "embed"

//go:embed init.sql
var InitSQL string

//go:embed migrations/002_security_retention.sql
var SecurityRetentionSQL string

//go:embed migrations/003_host_health.sql
var HostHealthSQL string

//go:embed migrations/legacy_text_to_numeric.sql
var LegacyTextToNumericSQL string
