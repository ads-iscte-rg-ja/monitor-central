ALTER TABLE hosts ADD COLUMN api_key_digest TEXT;
ALTER TABLE hosts ADD COLUMN api_key_masked TEXT;
CREATE TABLE api_key_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), secret_fingerprint TEXT NOT NULL
);
-- Previous sessions may contain a raw key waiting for its one-time display.
DELETE FROM sessions;
-- Receipts deliberately outlive telemetry payloads, preventing a pruned retry
-- from becoming a new record or firing another alert. Deleted hosts cascade them.
CREATE TABLE telemetry_receipts (
 host_id BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('stat','error')),
 source_id BIGINT NOT NULL CHECK(source_id>0), record_id BIGINT NOT NULL,
 PRIMARY KEY(host_id,kind,source_id)
);
INSERT INTO telemetry_receipts(host_id,kind,source_id,record_id)
 SELECT host_id,'stat',source_id,id FROM machine_stats WHERE source_id IS NOT NULL;
INSERT INTO telemetry_receipts(host_id,kind,source_id,record_id)
 SELECT host_id,'error',source_id,id FROM error_logs WHERE source_id IS NOT NULL;
CREATE INDEX idx_machine_stats_retention ON machine_stats(collected_at,id);
CREATE INDEX idx_resolved_events_retention ON alert_events(resolved_at,id) WHERE status='resolved';
CREATE INDEX idx_sent_outbox_retention ON email_outbox(sent_at,id) WHERE sent_at IS NOT NULL;
