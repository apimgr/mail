package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MailForward is the public forwarding-rule model; it never exposes the
// internal mailbox key.
type MailForward struct {
	ID           string
	Source       string
	Destinations []string
	KeepLocal    bool
	Enabled      bool
	CreatedAt    int64
}

// ListMailForwards returns a bounded page of virtual-mailbox forwarding rules.
func (db *DB) ListMailForwards(ctx context.Context, page, limit int) ([]MailForward, int64, error) {
	if page < 1 || limit < 1 || limit > 250 {
		return nil, 0, fmt.Errorf("mail forward pagination is outside the allowed range")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const joins = ` FROM mail_forwards AS f JOIN mail_mailboxes AS m ON m.id = f.mailbox_id JOIN mail_domains AS d ON d.id = m.domain_id`
	var total int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT COUNT(*)`+joins).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count mail forwards: %w", err)
	}
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT f.public_id, m.local_part, d.domain, f.forward_to, f.keep_local, f.enabled, f.created_at`+joins+` ORDER BY d.domain, m.local_part, f.id LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list mail forwards: %w", err)
	}
	defer func() { _ = rows.Close() }()
	forwards := make([]MailForward, 0)
	for rows.Next() {
		var forward MailForward
		var localPart, domain, destinations string
		var keepLocal, enabled int
		if err := rows.Scan(&forward.ID, &localPart, &domain, &destinations, &keepLocal, &enabled, &forward.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("read mail forward: %w", err)
		}
		forward.Source = localPart + "@" + domain
		forward.Destinations = decodeForwardDestinations(destinations)
		forward.KeepLocal = keepLocal != 0
		forward.Enabled = enabled != 0
		forwards = append(forwards, forward)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mail forwards: %w", err)
	}
	return forwards, total, nil
}

// CreateMailForward stores a system-managed forwarding rule for an existing
// virtual mailbox.
func (db *DB) CreateMailForward(ctx context.Context, mailboxID string, destinations []string, keepLocal, enabled bool) (MailForward, error) {
	publicID, err := uuid.NewV7()
	if err != nil {
		return MailForward{}, fmt.Errorf("generate mail forward public ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	serializedDestinations, err := json.Marshal(destinations)
	if err != nil {
		return MailForward{}, fmt.Errorf("encode mail forward destinations: %w", err)
	}
	var forwardID int64
	err = db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		var internalMailboxID int64
		if err := tx.QueryRowContext(ctx, `SELECT m.id FROM mail_mailboxes AS m JOIN mail_domains AS d ON d.id = m.domain_id
			WHERE m.public_id = ? AND m.enabled = 1 AND d.enabled = 1`, mailboxID).Scan(&internalMailboxID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mail_forwards (public_id, mailbox_id, owner_type, owner_id, forward_to, keep_local, enabled, created_at)
			VALUES (?, ?, 'system', 'system', ?, ?, ?, ?)`, publicID.String(), internalMailboxID, string(serializedDestinations), boolToDB(keepLocal), boolToDB(enabled), time.Now().UTC().Unix())
		if err != nil {
			return fmt.Errorf("insert mail forward: %w", err)
		}
		forwardID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return MailForward{}, fmt.Errorf("create mail forward: %w", err)
	}
	return db.getMailForwardByInternalID(ctx, forwardID)
}

// DeleteMailForward removes a rule selected by its opaque public identifier.
func (db *DB) DeleteMailForward(ctx context.Context, publicID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM mail_forwards WHERE public_id = ?`, publicID)
		if err != nil {
			return fmt.Errorf("delete mail forward: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check deleted mail forward: %w", err)
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (db *DB) getMailForwardByInternalID(ctx context.Context, id int64) (MailForward, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var forward MailForward
	var localPart, domain, destinations string
	var keepLocal, enabled int
	err := db.UsersReadDB.QueryRowContext(ctx, `SELECT f.public_id, m.local_part, d.domain, f.forward_to, f.keep_local, f.enabled, f.created_at
		FROM mail_forwards AS f JOIN mail_mailboxes AS m ON m.id = f.mailbox_id JOIN mail_domains AS d ON d.id = m.domain_id WHERE f.id = ?`, id).
		Scan(&forward.ID, &localPart, &domain, &destinations, &keepLocal, &enabled, &forward.CreatedAt)
	if err != nil {
		return MailForward{}, fmt.Errorf("get mail forward: %w", err)
	}
	forward.Source = localPart + "@" + domain
	forward.Destinations = decodeForwardDestinations(destinations)
	forward.KeepLocal = keepLocal != 0
	forward.Enabled = enabled != 0
	return forward, nil
}

func decodeForwardDestinations(value string) []string {
	var destinations []string
	if err := json.Unmarshal([]byte(value), &destinations); err == nil {
		return destinations
	}
	parts := strings.Split(value, ",")
	destinations = make([]string, 0, len(parts))
	for _, part := range parts {
		if destination := strings.TrimSpace(part); destination != "" {
			destinations = append(destinations, destination)
		}
	}
	return destinations
}

func (db *DB) backfillMailForwardPublicIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT id FROM mail_forwards WHERE public_id IS NULL OR public_id = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list forwards missing public IDs: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read mail forward ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate mail forward IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close mail forward ID rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		for _, id := range ids {
			publicID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generate mail forward public ID: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_forwards SET public_id = ? WHERE id = ? AND (public_id IS NULL OR public_id = '')`, publicID.String(), id); err != nil {
				return fmt.Errorf("backfill mail forward public ID: %w", err)
			}
		}
		return nil
	})
}
