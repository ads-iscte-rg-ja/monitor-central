-- Last buffer snapshot reported by each host's agent (ADR-0010). One row per
-- host, replaced only by a newer observed_at (out-of-order batches, several
-- instances). dropped_by_kind holds cumulative counters by log level or
-- machine_stat (ADR-0009).
CREATE TABLE host_buffer_status (
 host_id BIGINT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
 observed_at TIMESTAMPTZ NOT NULL,
 pending BIGINT NOT NULL CHECK(pending >= 0),
 quarantined BIGINT NOT NULL CHECK(quarantined >= 0),
 oldest_pending_age_seconds BIGINT NOT NULL CHECK(oldest_pending_age_seconds >= 0),
 delivery_paused BOOLEAN NOT NULL,
 size_bytes BIGINT NOT NULL CHECK(size_bytes >= 0),
 dropped_by_kind JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(dropped_by_kind) = 'object'),
 received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schema_migrations(version) VALUES(5);
