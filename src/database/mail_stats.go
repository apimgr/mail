package database

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MailDeliveryStats contains delivery-log counts grouped by recorded status.
type MailDeliveryStats struct {
	Total    int64
	ByStatus map[string]int64
}

// MailSpamStats contains daily aggregate spam and virus detections.
type MailSpamStats struct {
	Spam        int64
	Virus       int64
	Granularity string
}

// GetMailDeliveryStats counts delivery-log records at or after since.
func (db *DB) GetMailDeliveryStats(ctx context.Context, since time.Time) (MailDeliveryStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stats := MailDeliveryStats{ByStatus: make(map[string]int64)}
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT lower(trim(status)), COUNT(*)
		FROM mail_delivery_log WHERE timestamp >= ? GROUP BY lower(trim(status)) ORDER BY lower(trim(status))`, since.UTC().Unix())
	if err != nil {
		return MailDeliveryStats{}, fmt.Errorf("query mail delivery statistics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return MailDeliveryStats{}, fmt.Errorf("read mail delivery statistics: %w", err)
		}
		status = strings.TrimSpace(status)
		if status == "" {
			status = "unknown"
		}
		stats.ByStatus[status] = count
		stats.Total += count
	}
	if err := rows.Err(); err != nil {
		return MailDeliveryStats{}, fmt.Errorf("iterate mail delivery statistics: %w", err)
	}
	return stats, nil
}

// GetMailSpamStats sums hourly or daily spam and virus counters for the selected period.
func (db *DB) GetMailSpamStats(ctx context.Context, since time.Time, period time.Duration) (MailSpamStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var stats MailSpamStats
	var err error
	if period <= 7*24*time.Hour {
		stats.Granularity = "hour"
		hourlySince := since.UTC().Truncate(time.Hour)
		err = db.UsersReadDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(spam_count), 0), COALESCE(SUM(virus_count), 0)
			FROM mail_stats_hourly WHERE hour_timestamp >= ?`, hourlySince.Unix()).Scan(&stats.Spam, &stats.Virus)
	} else {
		stats.Granularity = "day"
		dailySince := since.UTC().Truncate(24 * time.Hour)
		err = db.UsersReadDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(spam_count), 0), COALESCE(SUM(virus_count), 0)
			FROM mail_stats_daily WHERE day_timestamp >= ?`, dailySince.Unix()).Scan(&stats.Spam, &stats.Virus)
	}
	if err != nil {
		return MailSpamStats{}, fmt.Errorf("query mail spam statistics: %w", err)
	}
	return stats, nil
}
