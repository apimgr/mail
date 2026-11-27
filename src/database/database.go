package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"

	"github.com/apimgr/mail/src/config"
)

// DB wraps database connections per AI.md PART 10
// Single instance: server.db + users.db (SQLite)
// Cluster mode: shared PostgreSQL/MySQL connection
type DB struct {
	// ServerDB is the server.db write handle.
	ServerDB *sql.DB
	// ServerReadDB is the server.db read-only handle.
	ServerReadDB *sql.DB
	// UsersDB is the users.db write handle.
	UsersDB *sql.DB
	// UsersReadDB is the users.db read-only handle.
	UsersReadDB *sql.DB
	driver      string
}

// RequestStats contains persisted aggregate request counters.
type RequestStats struct {
	Total  int64
	Hourly map[int64]int64
}

// SchedulerTaskState is the durable state of a built-in scheduled task.
type SchedulerTaskState struct {
	ID         string
	Name       string
	Enabled    bool
	Schedule   string
	LastRun    int64
	NextRun    int64
	LastStatus string
	LastError  string
	RunCount   int64
	FailCount  int64
}

// Connect establishes database connections based on configuration
// Per AI.md PART 10: Connection pooling with timeouts
func Connect(cfg *config.Config) (*DB, error) {
	if cfg.Database.Driver == "libsql" && strings.TrimSpace(cfg.Database.DSN()) == "" {
		return nil, errors.New("libsql driver requires a remote database URL")
	}
	db := &DB{driver: cfg.Database.Driver}

	// Connect to server database
	serverDB, err := openDB(cfg.Database.DSN(), cfg.Database.Driver)
	if err != nil {
		return nil, fmt.Errorf("server database connection failed: %w", err)
	}
	db.ServerDB = serverDB
	db.ServerReadDB, err = openReadDB(cfg.Database.DSN(), cfg.Database.Driver)
	if err != nil {
		_ = serverDB.Close()
		return nil, fmt.Errorf("server database read connection failed: %w", err)
	}

	db.UsersDB = serverDB
	db.UsersReadDB = db.ServerReadDB

	// Configure connection pools per AI.md PART 10
	configurePool(db.ServerDB, cfg.Database.Driver)
	if cfg.Database.Driver == "sqlite" {
		configureReadPool(db.ServerReadDB, cfg.Database.Pool.MaxOpen)
	}

	return db, nil
}

// openDB establishes a single database connection with timeout
// Per AI.md PART 10: All queries MUST have timeouts
func openDB(dsn, driver string) (*sql.DB, error) {
	if driver == "sqlite" {
		dsn = sqliteDSN(dsn) + "&_txlock=immediate"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		if driver == "libsql" {
			return nil, fmt.Errorf("libSQL database driver could not be opened")
		}
		return nil, err
	}

	// Test connection with 5 second timeout per AI.md PART 10
	ctx, cancel := timeoutContext(5 * time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver == "libsql" {
			return nil, fmt.Errorf("libSQL database ping failed")
		}
		return nil, fmt.Errorf("database ping failed: %w", err)
	}
	if driver == "sqlite" {
		var mode string
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("SQLite WAL mode unavailable for %q (mode %q): %w", dsn, mode, err)
		}
		if !strings.EqualFold(mode, "wal") {
			_ = db.Close()
			return nil, fmt.Errorf("SQLite WAL mode unavailable for %q (mode %q)", dsn, mode)
		}
	}

	return db, nil
}

func openReadDB(dsn, driver string) (*sql.DB, error) {
	if driver != "sqlite" {
		return openDB(dsn, driver)
	}
	dsn = sqliteDSN(dsn) + "&_pragma=query_only(1)"
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := timeoutContext(5 * time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database read ping failed: %w", err)
	}
	return db, nil
}

func sqliteDSN(dsn string) string {
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	pragmas := []string{
		"busy_timeout(10000)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"foreign_keys(1)",
	}
	for _, pragma := range pragmas {
		key := "_pragma=" + strings.SplitN(pragma, "(", 2)[0] + "("
		if strings.Contains(dsn, key) {
			continue
		}
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + "_pragma=" + pragma
	}
	return dsn
}

// configurePool sets connection pool settings per driver type
// Per AI.md PART 10: Pool Configuration section
func configurePool(db *sql.DB, driver string) {
	switch driver {
	case "sqlite":
		// SQLite: Single connection (WAL mode allows multiple readers)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
	}
}

func configureReadPool(db *sql.DB, maxOpen int) {
	if maxOpen <= 0 {
		maxOpen = 25
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(0)
}

// Close closes all database connections
func (db *DB) Close() error {
	var errs []string

	if db.driver == "sqlite" && db.ServerDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := db.ServerDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("server db checkpoint: %v", err))
		}
	}
	if db.ServerReadDB != nil && db.ServerReadDB != db.ServerDB {
		if err := db.ServerReadDB.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("server read db: %v", err))
		}
	}
	if db.ServerDB != nil {
		if err := db.ServerDB.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("server db: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// EnsureSchema creates all database tables and applies updates
// Per AI.md PART 10: "All schema changes are idempotent and run on every startup"
// "CREATE TABLE IF NOT EXISTS for self-creating schema"
// "No migration files. No version tracking."
func (db *DB) EnsureSchema() error {
	// Create server database schema
	if err := db.ensureServerSchema(); err != nil {
		return fmt.Errorf("server db schema: %w", err)
	}
	if err := db.ensureMailSchema(); err != nil {
		return fmt.Errorf("mail schema: %w", err)
	}

	if err := db.EnsureGeneratedSecrets(context.Background()); err != nil {
		return fmt.Errorf("initialize generated secrets: %w", err)
	}

	return nil
}

func (db *DB) ensureMailSchema() error {
	for _, stmt := range mailCreateStatements {
		if err := db.execSchema(db.UsersDB, stmt); err != nil {
			return fmt.Errorf("create mail table: %w", err)
		}
	}
	for _, stmt := range mailSchemaUpdates {
		if err := db.execSchema(db.UsersDB, stmt); err != nil && !isColumnExistsError(err) {
			return fmt.Errorf("update mail schema: %w", err)
		}
	}
	if err := db.backfillMailDomainPublicIDs(); err != nil {
		return fmt.Errorf("backfill mail domain public IDs: %w", err)
	}
	if err := db.backfillMailAliasPublicIDs(); err != nil {
		return fmt.Errorf("backfill mail alias public IDs: %w", err)
	}
	if err := db.backfillMailMailboxPublicIDs(); err != nil {
		return fmt.Errorf("backfill virtual mailbox public IDs: %w", err)
	}
	if err := db.backfillMailForwardPublicIDs(); err != nil {
		return fmt.Errorf("backfill mail-forward public IDs: %w", err)
	}
	return nil
}

// ensureServerSchema creates server.db tables
// Per AI.md PART 10: Idempotent - safe to run multiple times
func (db *DB) ensureServerSchema() error {
	// 1. Create tables (idempotent with CREATE TABLE IF NOT EXISTS)
	for _, stmt := range serverCreateStatements {
		if err := db.execSchema(db.ServerDB, stmt); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO owners (id) VALUES ('system')`)
		return err
	}); err != nil {
		return fmt.Errorf("initialize system owner: %w", err)
	}
	if err := db.migrateLegacyAPITokenIDs(); err != nil {
		return fmt.Errorf("migrate API token identifiers: %w", err)
	}

	// 2. Apply schema updates (idempotent ALTER TABLE)
	for _, stmt := range serverSchemaUpdates {
		if err := db.execSchema(db.ServerDB, stmt); err != nil {
			// Per AI.md PART 10: Ignore "column already exists" errors
			if !isColumnExistsError(err) {
				return fmt.Errorf("schema update: %w", err)
			}
		}
	}

	return nil
}

// execSchema executes a schema statement with timeout
// Per AI.md PART 10: Migrations have 5 minute timeout
func (db *DB) execSchema(conn *sql.DB, statement string) error {
	ctx, cancel := timeoutContext(5 * time.Minute)
	defer cancel()
	if db.driver == "sqlite" && conn == db.ServerDB {
		return db.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, statement)
			return err
		})
	}

	_, err := conn.ExecContext(ctx, statement)
	if err == context.DeadlineExceeded {
		return errors.New("TIMEOUT: Schema statement timed out")
	}
	return err
}

// WithTransaction executes a function within a transaction
// Per AI.md PART 10: Transaction Patterns section
func (db *DB) WithTransaction(ctx context.Context, conn *sql.DB, fn func(*sql.Tx) error) error {
	if db.driver == "sqlite" && conn == db.ServerDB {
		return db.Write(ctx, fn)
	}
	// Transaction timeout: 30 seconds per AI.md PART 10
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

// Write executes a short SQLite write transaction with bounded busy retries.
func (db *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	started := time.Now()
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = db.writeOnce(ctx, fn)
		if !isBusyError(err) {
			if time.Since(started) > time.Second {
				_, file, line, _ := runtime.Caller(1)
				slog.Warn("db.busy", "wait_ms", time.Since(started).Milliseconds(), "caller", fmt.Sprintf("%s:%d", filepath.Base(file), line))
			}
			return err
		}
		wait := time.Duration(25<<attempt)*time.Millisecond + time.Duration(rand.Intn(25))*time.Millisecond
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	if time.Since(started) > time.Second {
		_, file, line, _ := runtime.Caller(1)
		slog.Warn("db.busy", "wait_ms", time.Since(started).Milliseconds(), "caller", fmt.Sprintf("%s:%d", filepath.Base(file), line))
	}
	return err
}

func (db *DB) writeOnce(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.ServerDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// LoadRequestStats returns the persisted total and the last 24 hourly buckets.
func (db *DB) LoadRequestStats(ctx context.Context, now time.Time) (RequestStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stats := RequestStats{Hourly: make(map[int64]int64)}
	if err := db.ServerReadDB.QueryRowContext(ctx, "SELECT total_requests FROM request_stats WHERE id = 1").Scan(&stats.Total); err != nil {
		return RequestStats{}, fmt.Errorf("load total request count: %w", err)
	}
	cutoff := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour).Unix()
	rows, err := db.ServerReadDB.QueryContext(ctx, "SELECT hour_start, request_count FROM request_stats_hourly WHERE hour_start >= ?", cutoff)
	if err != nil {
		return RequestStats{}, fmt.Errorf("load hourly request counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var hour, count int64
		if err := rows.Scan(&hour, &count); err != nil {
			return RequestStats{}, fmt.Errorf("scan hourly request count: %w", err)
		}
		stats.Hourly[hour] = count
	}
	if err := rows.Err(); err != nil {
		return RequestStats{}, fmt.Errorf("read hourly request counts: %w", err)
	}
	return stats, nil
}

// FlushRequestStats persists a batch of request counters atomically.
func (db *DB) FlushRequestStats(ctx context.Context, total int64, hourly map[int64]int64, now time.Time) error {
	if total == 0 && len(hourly) == 0 {
		return nil
	}
	return db.WithTransaction(ctx, db.ServerDB, func(tx *sql.Tx) error {
		if total > 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE request_stats SET total_requests = total_requests + ? WHERE id = 1", total); err != nil {
				return fmt.Errorf("update total request count: %w", err)
			}
		}
		for hour, count := range hourly {
			if count == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO request_stats_hourly (hour_start, request_count) VALUES (?, ?) ON CONFLICT(hour_start) DO UPDATE SET request_count = request_count + excluded.request_count", hour, count); err != nil {
				return fmt.Errorf("update hourly request count: %w", err)
			}
		}
		cutoff := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour).Unix()
		if _, err := tx.ExecContext(ctx, "DELETE FROM request_stats_hourly WHERE hour_start < ?", cutoff); err != nil {
			return fmt.Errorf("prune hourly request counts: %w", err)
		}
		return nil
	})
}

// LoadSchedulerTasks loads persisted scheduler state from server.db.
func (db *DB) LoadSchedulerTasks(ctx context.Context) (map[string]SchedulerTaskState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, `SELECT id, name, enabled, schedule,
		COALESCE(last_run, 0), COALESCE(next_run, 0), COALESCE(last_status, ''),
		COALESCE(last_error, ''), run_count, fail_count FROM scheduler_tasks`)
	if err != nil {
		return nil, fmt.Errorf("load scheduler tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	states := make(map[string]SchedulerTaskState)
	for rows.Next() {
		var state SchedulerTaskState
		var enabled int
		if err := rows.Scan(&state.ID, &state.Name, &enabled, &state.Schedule,
			&state.LastRun, &state.NextRun, &state.LastStatus, &state.LastError,
			&state.RunCount, &state.FailCount); err != nil {
			return nil, fmt.Errorf("scan scheduler task: %w", err)
		}
		state.Enabled = enabled != 0
		states[state.ID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read scheduler tasks: %w", err)
	}
	return states, nil
}

// SaveSchedulerTask persists one task state without removing its history.
func (db *DB) SaveSchedulerTask(ctx context.Context, state SchedulerTaskState) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	enabled := 0
	if state.Enabled {
		enabled = 1
	}
	statement := `INSERT INTO scheduler_tasks
			(id, name, enabled, schedule, last_run, next_run, last_status, last_error, run_count, fail_count)
			VALUES (?, ?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, enabled = excluded.enabled,
			schedule = excluded.schedule, last_run = excluded.last_run, next_run = excluded.next_run,
			last_status = excluded.last_status, last_error = excluded.last_error,
			run_count = excluded.run_count, fail_count = excluded.fail_count`
	return db.WithTransaction(ctx, db.ServerDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, statement, state.ID, state.Name, enabled, state.Schedule,
			state.LastRun, state.NextRun, state.LastStatus, state.LastError, state.RunCount, state.FailCount); err != nil {
			return fmt.Errorf("save scheduler task %q: %w", state.ID, err)
		}
		return nil
	})
}

// RecordSchedulerRun appends an immutable task execution result to history.
func (db *DB) RecordSchedulerRun(ctx context.Context, taskID string, startedAt, finishedAt time.Time, status, failure string, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	statement := `INSERT INTO scheduler_history (task_id, started_at, finished_at, status, error, duration_ms)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?)`
	args := []any{taskID, startedAt.Unix(), finishedAt.Unix(), status, failure, duration.Milliseconds()}
	if _, err := db.ServerDB.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("record scheduler task %q history: %w", taskID, err)
	}
	return nil
}

// WithSerializableRetry executes a function with serializable isolation and retry logic
// Per AI.md PART 10: Retry on Serialization Failure section
func (db *DB) WithSerializableRetry(ctx context.Context, conn *sql.DB, maxRetries int, fn func(*sql.Tx) error) error {
	if db.driver == "sqlite" && conn == db.ServerDB {
		return db.Write(ctx, fn)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := func() error {
			tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			if isSerializationError(err) && attempt < maxRetries-1 {
				wait := time.Duration(attempt+1) * 10 * time.Millisecond
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
					continue
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				}
			}
			return err
		}
		return nil
	}
	return errors.New("max retries exceeded")
}
