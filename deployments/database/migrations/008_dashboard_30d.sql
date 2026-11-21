-- Dashboard queries with a bounded cost over 30 days of data (ADR-0017).

-- Error counts per UTC hour, host and level: today's errors are a sum of at
-- most 24 x hosts x levels rows instead of a COUNT(*) over millions. Statement
-- triggers keep them in the same transaction as every write to error_logs
-- (ingestion, demo seed, tests), so they cannot diverge.
CREATE TABLE error_counts_hourly (
 bucket TIMESTAMPTZ NOT NULL,
 host_id BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
 level TEXT NOT NULL,
 count BIGINT NOT NULL CHECK(count >= 0),
 PRIMARY KEY(bucket, host_id, level)
);

-- Rows are upserted in key order, so concurrent batches lock them in the same
-- order and cannot deadlock.
CREATE FUNCTION error_counts_add() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO error_counts_hourly(bucket, host_id, level, count)
 SELECT date_bin('1 hour', created_at, TIMESTAMPTZ '2000-01-01 00:00:00+00'), host_id, level, COUNT(*)
 FROM new_rows GROUP BY 1, 2, 3 ORDER BY 1, 2, 3
 ON CONFLICT(bucket, host_id, level) DO UPDATE SET count = error_counts_hourly.count + EXCLUDED.count;
 RETURN NULL;
END $$;

CREATE FUNCTION error_counts_remove() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 WITH removed AS (
  SELECT date_bin('1 hour', created_at, TIMESTAMPTZ '2000-01-01 00:00:00+00') AS bucket, host_id, level, COUNT(*) AS n
  FROM old_rows GROUP BY 1, 2, 3 ORDER BY 1, 2, 3)
 UPDATE error_counts_hourly c SET count = GREATEST(c.count - removed.n, 0)
 FROM removed WHERE c.bucket = removed.bucket AND c.host_id = removed.host_id AND c.level = removed.level;
 RETURN NULL;
END $$;

CREATE TRIGGER error_counts_on_insert AFTER INSERT ON error_logs
 REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION error_counts_add();
CREATE TRIGGER error_counts_on_delete AFTER DELETE ON error_logs
 REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION error_counts_remove();
-- An update (the demo seed's ON CONFLICT ... DO UPDATE) moves a row between
-- hours: remove the old version, add the new one.
CREATE TRIGGER error_counts_on_update_remove AFTER UPDATE ON error_logs
 REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION error_counts_remove();
CREATE TRIGGER error_counts_on_update_add AFTER UPDATE ON error_logs
 REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION error_counts_add();

INSERT INTO error_counts_hourly(bucket, host_id, level, count)
SELECT date_bin('1 hour', created_at, TIMESTAMPTZ '2000-01-01 00:00:00+00'), host_id, level, COUNT(*)
FROM error_logs GROUP BY 1, 2, 3;

-- Covering index: a host's metrics and each host's latest sample are read from
-- the index alone, in host and time order, without one random heap read per
-- row. Same key as before, so the write path keeps the same number of indexes.
DROP INDEX idx_machine_stats_host_collected;
CREATE INDEX idx_machine_stats_host_collected ON machine_stats(host_id, collected_at DESC)
 INCLUDE (cpu_percent, ram_used_mb, ram_total_mb, disk_used_gb, disk_total_gb);

INSERT INTO schema_migrations(version) VALUES(8);
