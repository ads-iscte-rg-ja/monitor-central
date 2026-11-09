-- Executed only for the previous TEXT-ID schema, within one transaction.
-- Encoded session user IDs change type, so old sessions are not copied.
CREATE SCHEMA monitor_legacy;
ALTER TABLE users SET SCHEMA monitor_legacy;
ALTER TABLE audit_log SET SCHEMA monitor_legacy;
ALTER TABLE sessions SET SCHEMA monitor_legacy;
ALTER TABLE hosts SET SCHEMA monitor_legacy;
ALTER TABLE machine_stats SET SCHEMA monitor_legacy;
ALTER TABLE error_logs SET SCHEMA monitor_legacy;
ALTER TABLE alerts SET SCHEMA monitor_legacy;
ALTER TABLE alert_events SET SCHEMA monitor_legacy;
-- CREATE_NEW_SCHEMA
CREATE TEMP TABLE user_map ON COMMIT DROP AS SELECT id AS old_id,row_number() OVER(ORDER BY created_at,id)::BIGINT AS id FROM monitor_legacy.users;
CREATE TEMP TABLE host_map ON COMMIT DROP AS SELECT id AS old_id,row_number() OVER(ORDER BY created_at,id)::BIGINT AS id FROM monitor_legacy.hosts;
CREATE TEMP TABLE alert_map ON COMMIT DROP AS SELECT id AS old_id,row_number() OVER(ORDER BY created_at,id)::BIGINT AS id FROM monitor_legacy.alerts;
INSERT INTO users(id,email,notification_email,name,password_hash,created_at,last_login_at)
SELECT m.id,u.email,u.notification_email,COALESCE(u.name,''),u.password_hash,u.created_at,u.last_login_at FROM monitor_legacy.users u JOIN user_map m ON m.old_id=u.id;
INSERT INTO hosts(id,name,hostname,api_key,status,last_seen_at,created_at,demo)
SELECT m.id,h.name,h.hostname,h.api_key,h.status,h.last_seen_at,h.created_at,h.id LIKE 'demo-host-%' FROM monitor_legacy.hosts h JOIN host_map m ON m.old_id=h.id;
INSERT INTO audit_log(user_id,action,ip_address,timestamp)
SELECT m.id,a.action,a.ip_address,a.timestamp FROM monitor_legacy.audit_log a JOIN user_map m ON m.old_id=a.user_id ORDER BY a.timestamp,a.id;
INSERT INTO machine_stats(host_id,cpu_percent,ram_used_mb,ram_total_mb,disk_used_gb,disk_total_gb,collected_at)
SELECT m.id,s.cpu_percent,s.ram_used_mb,s.ram_total_mb,s.disk_used_gb,s.disk_total_gb,LEAST(s.collected_at,CURRENT_TIMESTAMP) FROM monitor_legacy.machine_stats s JOIN host_map m ON m.old_id=s.host_id ORDER BY s.collected_at,s.id;
INSERT INTO error_logs(host_id,app_name,environment,level,message,stack_trace,metadata,created_at)
SELECT m.id,e.app_name,e.environment,e.level,e.message,e.stack_trace,e.metadata,LEAST(e.created_at,CURRENT_TIMESTAMP) FROM monitor_legacy.error_logs e JOIN host_map m ON m.old_id=e.host_id ORDER BY e.created_at,e.id;
INSERT INTO alerts(id,name,host_id,metric,condition,threshold,severity,notification_channels,enabled,system_rule,created_at,updated_at)
SELECT a.id,r.name,h.id,r.metric,r.condition,r.threshold,r.severity,r.notification_channels,r.enabled,r.id LIKE 'host-offline-%',r.created_at,r.updated_at FROM monitor_legacy.alerts r JOIN alert_map a ON a.old_id=r.id LEFT JOIN host_map h ON h.old_id=r.host_id;
-- Preserve duplicate history, resolving surplus open incidents for each pair.
WITH migrated AS (
 SELECT e.*,a.id AS new_alert_id,h.id AS new_host_id,
 row_number() OVER(PARTITION BY a.id,h.id,(e.status IN ('fired','acknowledged')) ORDER BY e.fired_at DESC,e.id DESC) AS position
 FROM monitor_legacy.alert_events e JOIN alert_map a ON a.old_id=e.alert_id JOIN host_map h ON h.old_id=e.host_id
)
INSERT INTO alert_events(alert_id,host_id,triggered_value,status,fired_at,resolved_at)
SELECT new_alert_id,new_host_id,triggered_value,CASE WHEN status IN ('fired','acknowledged') AND position>1 THEN 'resolved' ELSE status END,
fired_at,CASE WHEN status IN ('fired','acknowledged') AND position>1 THEN CURRENT_TIMESTAMP ELSE resolved_at END FROM migrated ORDER BY fired_at,id;
SELECT setval(pg_get_serial_sequence('users','id'),COALESCE(MAX(id),1),COUNT(*)>0) FROM users;
SELECT setval(pg_get_serial_sequence('hosts','id'),COALESCE(MAX(id),1),COUNT(*)>0) FROM hosts;
SELECT setval(pg_get_serial_sequence('alerts','id'),COALESCE(MAX(id),1),COUNT(*)>0) FROM alerts;
-- All records have been copied with mapped foreign keys; remove the old tables
-- and plaintext-password column. Any error rolls the entire migration back.
DROP SCHEMA monitor_legacy CASCADE;
