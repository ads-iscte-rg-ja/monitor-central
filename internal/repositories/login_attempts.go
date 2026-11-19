package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/lib/pq"

	"monitor-central/internal/database"
)

// LoginAttemptsRepository keeps the login limiter's windows in PostgreSQL, so
// every instance of the central counts against the same limit (ADR-0007).
type LoginAttemptsRepository struct{ db *database.DB }

func NewLoginAttemptsRepository(db *database.DB) *LoginAttemptsRepository {
	return &LoginAttemptsRepository{db: db}
}

// CountAttempts adds one attempt to each key's window, starting a new window
// for keys whose window has ended, in a single statement. Keys must be
// distinct: one INSERT … ON CONFLICT cannot update the same row twice.
func (r *LoginAttemptsRepository) CountAttempts(ctx context.Context, keys [][]byte, window time.Duration) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `INSERT INTO login_attempts(key,attempts,window_ends)
 SELECT key, 1, CURRENT_TIMESTAMP + $2 * INTERVAL '1 millisecond' FROM unnest($1::bytea[]) AS t(key)
 ON CONFLICT(key) DO UPDATE SET
  attempts = CASE WHEN login_attempts.window_ends <= CURRENT_TIMESTAMP THEN 1 ELSE login_attempts.attempts + 1 END,
  window_ends = CASE WHEN login_attempts.window_ends <= CURRENT_TIMESTAMP THEN EXCLUDED.window_ends ELSE login_attempts.window_ends END
 RETURNING key, attempts`, pq.ByteaArray(keys), window.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := make(map[string]int, len(keys))
	for rows.Next() {
		var key []byte
		var attempts int
		if err := rows.Scan(&key, &attempts); err != nil {
			return nil, err
		}
		byKey[string(key)] = attempts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts := make([]int, len(keys))
	for i, key := range keys {
		n, ok := byKey[string(key)]
		if !ok {
			return nil, fmt.Errorf("login attempts: no count returned for key %d", i)
		}
		counts[i] = n
	}
	return counts, nil
}
