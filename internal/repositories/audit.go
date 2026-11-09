package repositories

import (
	"context"
	"log"

	"monitor-central/internal/database"
	"monitor-central/internal/models"
)

// AuditRepository handles audit log data access
type AuditRepository struct {
	db     *database.DB
	logger *log.Logger
}

// NewAuditRepository creates a new AuditRepository
func NewAuditRepository(db *database.DB, logger *log.Logger) *AuditRepository {
	return &AuditRepository{db: db, logger: logger}
}

// Create inserts a new audit log entry
func (r *AuditRepository) Create(ctx context.Context, userID int64, action, ipAddress string) error {
	_, err := r.db.Insert(ctx,
		`INSERT INTO audit_log (user_id, action, ip_address) VALUES ($1,$2,$3)`,
		userID, action, ipAddress,
	)
	return err
}

// GetByUserID retrieves all audit log entries for a user
func (r *AuditRepository) GetByUserID(ctx context.Context, userID int64) ([]models.AuditLogEntry, error) {
	var entries []models.AuditLogEntry
	err := r.db.Select(ctx, &entries,
		`SELECT id, user_id, action, ip_address, timestamp 
		 FROM audit_log 
		 WHERE user_id = $1 
		 ORDER BY timestamp DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// CleanupOldEntries removes audit log entries older than specified days
func (r *AuditRepository) CleanupOldEntries(ctx context.Context, days int) (int64, error) {
	result, err := r.db.Delete(ctx,
		`DELETE FROM audit_log 
		 WHERE timestamp < CURRENT_TIMESTAMP - INTERVAL '1 day' * $1`,
		days,
	)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
