ALTER TABLE hosts
 ADD COLUMN health_endpoint TEXT NOT NULL DEFAULT '' CHECK(length(health_endpoint)<=2048),
 ADD COLUMN contact_timeout_seconds INTEGER NOT NULL DEFAULT 300 CHECK(contact_timeout_seconds BETWEEN 60 AND 86400),
 ADD COLUMN health_checked_at TIMESTAMPTZ,
 ADD COLUMN health_check_ok BOOLEAN,
 ADD COLUMN unavailable_since TIMESTAMPTZ,
 ADD CONSTRAINT hosts_health_result_complete CHECK((health_checked_at IS NULL) = (health_check_ok IS NULL));

UPDATE hosts SET unavailable_since=last_seen_at WHERE status='offline';
CREATE INDEX idx_hosts_health_endpoint ON hosts(id) WHERE health_endpoint<>'';
INSERT INTO schema_migrations(version) VALUES(3);
