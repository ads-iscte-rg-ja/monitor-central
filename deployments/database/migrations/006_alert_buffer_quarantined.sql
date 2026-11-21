-- Alert metric buffer_quarantined (ADR-0010; the registry test of ADR-0008
-- compares this list with internal/alertmetric).
ALTER TABLE alerts DROP CONSTRAINT alerts_metric_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_metric_check
 CHECK(metric IN ('cpu','ram','disk','error_rate','error_level','host_offline','buffer_quarantined'));
INSERT INTO schema_migrations(version) VALUES(6);
