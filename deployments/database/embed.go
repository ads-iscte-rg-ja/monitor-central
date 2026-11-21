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

//go:embed migrations/004_login_attempts.sql
var LoginAttemptsSQL string

//go:embed migrations/005_host_buffer_status.sql
var HostBufferStatusSQL string

//go:embed migrations/006_alert_buffer_quarantined.sql
var AlertBufferQuarantinedSQL string

//go:embed migrations/007_host_health_schedule.sql
var HostHealthScheduleSQL string

//go:embed migrations/legacy_text_to_numeric.sql
var LegacyTextToNumericSQL string
