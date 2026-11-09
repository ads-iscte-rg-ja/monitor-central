package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"monitor-central/internal/config"

	_ "github.com/lib/pq"
)

// DB wraps sql.DB with logging and helper methods
type DB struct {
	*sql.DB
	logger *log.Logger
}

// New creates a new database connection from config
func New(cfg *config.DBConfig, logger *log.Logger) (*DB, error) {
	// Build connection string with additional parameters
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password), Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Name}
	query := u.Query()
	query.Set("sslmode", cfg.SSLMode)
	query.Set("application_name", "monitor-central")
	query.Set("connect_timeout", "5")
	// Startup parameters apply to every connection, including replacements.
	query.Set("statement_timeout", "30000")
	query.Set("lock_timeout", "10000")
	query.Set("idle_in_transaction_session_timeout", "60000")
	u.RawQuery = query.Encode()
	dsn := u.String()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)       // Max open connections (default: 25)
	db.SetMaxIdleConns(cfg.MaxIdleConns)       // Max idle connections (default: 5)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime) // Max connection lifetime (default: 5m)
	db.SetConnMaxIdleTime(2 * time.Minute)     // Close idle connections after 2 min

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Println("Database connection established")

	return &DB{DB: db, logger: logger}, nil
}

// HealthCheck checks if the database is reachable
func (db *DB) HealthCheck(ctx context.Context) error {
	return db.PingContext(ctx)
}

// Close closes the database connection
func (db *DB) Close() error {
	db.logger.Println("Closing database connection")
	return db.DB.Close()
}

// Get retrieves a single row and scans into dest
// dest can be a pointer to a struct with `db:"column_name"` tags, or a pointer to a primitive type (string, int, []byte, bool, etc.)
func (db *DB) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}

	elem := v.Elem()

	if elem.Kind() != reflect.Struct {
		return db.DB.QueryRowContext(ctx, query, args...).Scan(dest)
	}

	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Get column names from query result
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Check if we have a row
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}

	// Build column to field index map
	fieldMap := buildFieldMap(elem.Type())

	// Create scan destinations based on columns
	scanDest := make([]interface{}, len(columns))
	for i, col := range columns {
		if fieldIdx, ok := fieldMap[col]; ok {
			scanDest[i] = elem.Field(fieldIdx).Addr().Interface()
		} else {
			// Column not mapped to struct field, use a placeholder
			var placeholder interface{}
			scanDest[i] = &placeholder
		}
	}

	return rows.Scan(scanDest...)
}

// Select retrieves multiple rows and scans into dest slice
// dest must be a pointer to a slice of structs with `db:"column_name"` tags
func (db *DB) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	return scanRows(rows, dest)
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE)
func (db *DB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.DB.ExecContext(ctx, query, args...)
}

// Insert is a convenience wrapper for INSERT queries
func (db *DB) Insert(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.Exec(ctx, query, args...)
}

// Update is a convenience wrapper for UPDATE queries
func (db *DB) Update(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.Exec(ctx, query, args...)
}

// Delete is a convenience wrapper for DELETE queries
func (db *DB) Delete(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.Exec(ctx, query, args...)
}

// scanRows scans multiple rows into a slice of structs
func scanRows(rows *sql.Rows, dest interface{}) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("dest must be a pointer to a slice")
	}

	sliceVal := v.Elem()
	elemType := sliceVal.Type().Elem()

	// Handle pointer to struct vs struct
	isPtr := elemType.Kind() == reflect.Ptr
	if isPtr {
		elemType = elemType.Elem()
	}

	if elemType.Kind() != reflect.Struct {
		return fmt.Errorf("slice element must be a struct or pointer to struct")
	}

	// Get column names from query result
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Build column to field index map
	fieldMap := buildFieldMap(elemType)

	for rows.Next() {
		// Create new struct instance
		newElem := reflect.New(elemType).Elem()

		// Create scan destinations based on columns
		scanDest := make([]interface{}, len(columns))
		for i, col := range columns {
			if fieldIdx, ok := fieldMap[col]; ok {
				scanDest[i] = newElem.Field(fieldIdx).Addr().Interface()
			} else {
				// Column not mapped to struct field, use a placeholder
				var placeholder interface{}
				scanDest[i] = &placeholder
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			return err
		}

		// Append to slice
		if isPtr {
			sliceVal.Set(reflect.Append(sliceVal, newElem.Addr()))
		} else {
			sliceVal.Set(reflect.Append(sliceVal, newElem))
		}
	}

	return rows.Err()
}

// buildFieldMap creates a map from column name to struct field index
func buildFieldMap(t reflect.Type) map[string]int {
	m := make(map[string]int)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if tag := field.Tag.Get("db"); tag != "" && tag != "-" {
			// Handle tags like `db:"column_name,omitempty"` - take first part
			tagName := strings.Split(tag, ",")[0]
			m[tagName] = i
		}
	}
	return m
}
