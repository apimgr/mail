package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AuditEvent is a secret-safe, structured record of a security-relevant action.
type AuditEvent struct {
	Category   string
	Action     string
	ActorType  string
	ActorID    string
	ActorIP    string
	TargetType string
	TargetID   string
	Details    map[string]any
	Success    bool
}

// AppendAuditEvent stores one audit record in server.db.
func (db *DB) AppendAuditEvent(ctx context.Context, event AuditEvent) error {
	if event.Category == "" || event.Action == "" {
		return fmt.Errorf("audit event category and action are required")
	}
	details, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}
	success := 0
	if event.Success {
		success = 1
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO audit_log
			(timestamp, category, action, actor_type, actor_id, actor_ip, target_type, target_id, details, success)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, time.Now().UTC().Unix(), event.Category,
			event.Action, event.ActorType, nullableString(event.ActorID), nullableString(event.ActorIP),
			nullableString(event.TargetType), nullableString(event.TargetID), string(details), success)
		if err != nil {
			return fmt.Errorf("append audit event: %w", err)
		}
		return nil
	})
}

// HasAuditEvent reports whether an event already exists for a stable target.
func (db *DB) HasAuditEvent(ctx context.Context, category, action, targetType, targetID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var found bool
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM audit_log
		WHERE category = ? AND action = ? AND target_type = ? AND target_id = ?)`, category, action, targetType, targetID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check audit event: %w", err)
	}
	return found, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
