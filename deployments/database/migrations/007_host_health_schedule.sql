-- Per-host probe schedule (ADR-0013): the health job claims only the hosts whose
-- next check is due. NULL means due now (a new host, or an edited endpoint).
ALTER TABLE hosts ADD COLUMN next_check_at TIMESTAMPTZ;
CREATE INDEX idx_hosts_health_due ON hosts(next_check_at NULLS FIRST, id) WHERE health_endpoint <> '';
INSERT INTO schema_migrations(version) VALUES(7);
