package seed

import (
	"context"
	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/services"
)

func Admin(ctx context.Context, db *database.DB, admin *config.AdminConfig) (int64, error) {
	hash, err := services.HashPassword(admin.Password)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users(email,name,password_hash,notification_email) VALUES($1,$2,$3,$4)
 ON CONFLICT(email) DO UPDATE SET name=EXCLUDED.name,password_hash=EXCLUDED.password_hash RETURNING id`, admin.Email, admin.Name, hash, admin.NotificationEmail).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=$1", id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
