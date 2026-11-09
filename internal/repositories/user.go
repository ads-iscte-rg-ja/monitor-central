package repositories

import (
	"context"
	"database/sql"
	"strings"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
)

// UserRepository handles user data access
type UserRepository struct {
	db *database.DB
}

// NewUserRepository creates a new UserRepository
func NewUserRepository(db *database.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user into the database
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (email, name, password_hash) VALUES ($1,$2,$3) RETURNING id,created_at`,
		user.Email, user.Name, user.PasswordHash,
	).Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "23505") || strings.Contains(errStr, "duplicate key") || strings.Contains(errStr, "UNIQUE constraint failed") {
			return models.ErrEmailExists
		}
		return err
	}
	return nil
}

// GetByID retrieves a user by ID
// GetByID retrieves a user by ID
func (r *UserRepository) GetByID(
	ctx context.Context,
	userID int64,
) (*models.User, error) {
	var user models.User

	err := r.db.Get(
		ctx,
		&user,
		`SELECT
			id,
			email,
			notification_email,
			name,
			created_at,
			last_login_at
		 FROM users
		 WHERE id = $1`,
		userID,
	)
	if err == sql.ErrNoRows {
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetByEmail retrieves a user by email (includes password hash for authentication)
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.db.Get(ctx, &user,
		"SELECT id, email, password_hash FROM users WHERE email = $1",
		email,
	)
	if err == sql.ErrNoRows {
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Exists checks if a user exists by ID
func (r *UserRepository) Exists(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.Get(ctx, &exists,
		"SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)",
		userID,
	)
	return exists, err
}

// UpdateLastLogin updates the user's last login timestamp
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID int64) error {
	_, err := r.db.Update(ctx,
		"UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = $1",
		userID,
	)
	return err
}

// Delete removes a user from the database
func (r *UserRepository) Delete(ctx context.Context, userID int64) (bool, error) {
	result, err := r.db.Delete(ctx,
		"DELETE FROM users WHERE id = $1",
		userID,
	)
	if err != nil {
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// UpdatePassword stores only the password hash.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := r.db.Update(ctx,
		"UPDATE users SET password_hash = $1 WHERE id = $2",
		passwordHash, userID,
	)
	return err
}

// UpdateNotificationEmail updates the address used for alerts.
func (r *UserRepository) UpdateNotificationEmail(
	ctx context.Context,
	userID int64,
	notificationEmail string,
) error {
	_, err := r.db.Update(
		ctx,
		`UPDATE users
		 SET notification_email = $1
		 WHERE id = $2`,
		notificationEmail,
		userID,
	)

	return err
}
