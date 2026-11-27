package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type RateLimitRecord struct {
	Key         string
	Count       int64
	Previous    int64
	WindowStart int64
}

func (db *DB) LoadRateLimits(ctx context.Context) ([]RateLimitRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, "SELECT key, count, prev_count, window_start FROM rate_limits")
	if err != nil {
		return nil, fmt.Errorf("load rate-limit counters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]RateLimitRecord, 0)
	for rows.Next() {
		var record RateLimitRecord
		if err := rows.Scan(&record.Key, &record.Count, &record.Previous, &record.WindowStart); err != nil {
			return nil, fmt.Errorf("read rate-limit counter: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rate-limit counters: %w", err)
	}
	return records, nil
}

func (db *DB) FlushRateLimits(ctx context.Context, records []RateLimitRecord, now time.Time) error {
	if len(records) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.ServerDB, func(tx *sql.Tx) error {
		statement := `INSERT INTO rate_limits (key, count, prev_count, window_start, updated_at)
				VALUES (?, ?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET count=excluded.count, prev_count=excluded.prev_count, window_start=excluded.window_start, updated_at=excluded.updated_at`
		for _, record := range records {
			if _, err := tx.ExecContext(ctx, statement, record.Key, record.Count, record.Previous, record.WindowStart, now.UTC().Unix()); err != nil {
				return fmt.Errorf("save rate-limit counter: %w", err)
			}
		}
		return nil
	})
}
