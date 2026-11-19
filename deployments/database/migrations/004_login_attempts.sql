-- Login limiter windows shared by every instance of the central (ADR-0007).
-- key is the SHA-256 of "ip:<address>" or "account:<e-mail>"; the retention job
-- deletes windows that have ended.
CREATE TABLE login_attempts (
 key BYTEA PRIMARY KEY CHECK(length(key)=32),
 attempts INTEGER NOT NULL CHECK(attempts > 0),
 window_ends TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_login_attempts_window_ends ON login_attempts(window_ends);
INSERT INTO schema_migrations(version) VALUES(4);
