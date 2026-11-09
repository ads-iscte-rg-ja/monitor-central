package repositories

import (
	"context"
	"log"

	"monitor-central/internal/database"

	"github.com/alexedwards/scs/postgresstore"
)

// SessionStore wraps postgresstore with custom user tracking capabilities.
type SessionStore struct {
	*postgresstore.PostgresStore
	db     *database.DB
	logger *log.Logger
}

// NewSessionStore creates a new session store using postgresstore as the foundation.
func NewSessionStore(db *database.DB, logger *log.Logger) *SessionStore {
	// Use the standard postgresstore for all SCS Store interface methods
	pgStore := postgresstore.New(db.DB)

	return &SessionStore{
		PostgresStore: pgStore,
		db:            db,
		logger:        logger,
	}
}

// UpdateUserID links a session token to a user ID in the database.
func (s *SessionStore) UpdateUserID(ctx context.Context, token string, userID int64) error {
	// s.logger.Printf("DEBUG: UpdateUserID called for token=%s user=%s", token, userID)

	_, err := s.db.Update(ctx,
		"UPDATE sessions SET user_id = $1 WHERE token = $2",
		userID, token,
	)
	if err != nil {
		s.logger.Printf("DEBUG: UpdateUserID failed: %v", err)
		return err
	}

	// rows, _ := result.RowsAffected()
	// s.logger.Printf("DEBUG: UpdateUserID completed. Rows affected: %d", rows)

	return nil
}

// DeleteAllForUser invalidates all sessions for a specific user.
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID int64) error {
	result, err := s.db.Delete(ctx,
		"DELETE FROM sessions WHERE user_id = $1",
		userID,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		s.logger.Printf("Invalidated %d sessions for user %d", rows, userID)
	}
	return nil
}
