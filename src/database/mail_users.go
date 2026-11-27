package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMailUserConflict        = errors.New("mail user already exists")
	ErrMailUserHasDependencies = errors.New("mail user has dependent records")
)

// MailUser is a virtual mailbox record without its password verifier.
type MailUser struct {
	ID         string
	Email      string
	LocalPart  string
	Domain     string
	QuotaBytes int64
	Enabled    bool
	CreatedAt  int64
	UpdatedAt  int64
}

// ListMailUsers returns a bounded page of virtual mailboxes.
func (db *DB) ListMailUsers(ctx context.Context, page, limit int) ([]MailUser, int64, error) {
	if page < 1 || limit < 1 || limit > 250 {
		return nil, 0, fmt.Errorf("mail user pagination is outside the allowed range")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const joins = ` FROM mail_mailboxes AS m JOIN mail_domains AS d ON d.id = m.domain_id`
	var total int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT COUNT(*)`+joins).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count mail users: %w", err)
	}
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT m.public_id, m.local_part, d.domain, m.quota_bytes, m.enabled, m.created_at, m.updated_at`+joins+` ORDER BY d.domain, m.local_part, m.id LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list mail users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	users := make([]MailUser, 0)
	for rows.Next() {
		var mailUser MailUser
		var enabled int
		if err := rows.Scan(&mailUser.ID, &mailUser.LocalPart, &mailUser.Domain, &mailUser.QuotaBytes, &enabled, &mailUser.CreatedAt, &mailUser.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("read mail user: %w", err)
		}
		mailUser.Email = mailUser.LocalPart + "@" + mailUser.Domain
		mailUser.Enabled = enabled != 0
		users = append(users, mailUser)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mail users: %w", err)
	}
	return users, total, nil
}

// GetMailUser returns one virtual mailbox by its opaque identifier.
func (db *DB) GetMailUser(ctx context.Context, publicID string) (MailUser, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var mailUser MailUser
	var enabled int
	err := db.UsersReadDB.QueryRowContext(ctx, `SELECT m.public_id, m.local_part, d.domain, m.quota_bytes, m.enabled, m.created_at, m.updated_at
		FROM mail_mailboxes AS m JOIN mail_domains AS d ON d.id = m.domain_id WHERE m.public_id = ?`, publicID).
		Scan(&mailUser.ID, &mailUser.LocalPart, &mailUser.Domain, &mailUser.QuotaBytes, &enabled, &mailUser.CreatedAt, &mailUser.UpdatedAt)
	if err != nil {
		return MailUser{}, fmt.Errorf("get mail user: %w", err)
	}
	mailUser.Email = mailUser.LocalPart + "@" + mailUser.Domain
	mailUser.Enabled = enabled != 0
	return mailUser, nil
}

// GetMailUserByEmail resolves an enabled virtual mailbox by its normalized
// address for forwarding-rule creation.
func (db *DB) GetMailUserByEmail(ctx context.Context, email string) (MailUser, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var user MailUser
	var enabled int
	err := db.UsersReadDB.QueryRowContext(ctx, `SELECT m.public_id, m.local_part, d.domain, m.quota_bytes, m.enabled, m.created_at, m.updated_at
		FROM mail_mailboxes AS m JOIN mail_domains AS d ON d.id = m.domain_id
		WHERE m.local_part || '@' || d.domain = ? AND m.enabled = 1 AND d.enabled = 1`, email).
		Scan(&user.ID, &user.LocalPart, &user.Domain, &user.QuotaBytes, &enabled, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return MailUser{}, fmt.Errorf("get mail user by email: %w", err)
	}
	user.Email = user.LocalPart + "@" + user.Domain
	user.Enabled = enabled != 0
	return user, nil
}

// CreateMailUser stores a virtual mailbox with its already-hashed password.
func (db *DB) CreateMailUser(ctx context.Context, localPart, domain, passwordHash string, quotaBytes int64, enabled bool) (MailUser, error) {
	publicID, err := uuid.NewV7()
	if err != nil {
		return MailUser{}, fmt.Errorf("generate mail user public ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Unix()
	var internalID int64
	err = db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		var domainID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mail_domains WHERE domain = ? AND enabled = 1`, domain).Scan(&domainID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mail_mailboxes
			(public_id, domain_id, owner_type, owner_id, local_part, password, quota_bytes, enabled, created_at, updated_at)
			VALUES (?, ?, 'system', 'system', ?, ?, ?, ?, ?, ?)`,
			publicID.String(), domainID, localPart, passwordHash, quotaBytes, boolToDB(enabled), now, now)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed: mail_mailboxes.domain_id, mail_mailboxes.local_part") {
				return ErrMailUserConflict
			}
			return fmt.Errorf("insert mail user: %w", err)
		}
		internalID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return MailUser{}, fmt.Errorf("create mail user: %w", err)
	}
	return db.getMailUserByInternalID(ctx, internalID)
}

// UpdateMailUserFields updates only the supplied mutable mailbox fields.
func (db *DB) UpdateMailUserFields(ctx context.Context, publicID string, quotaBytes *int64, enabled *bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var quotaValue, enabledValue any
	if quotaBytes != nil {
		quotaValue = *quotaBytes
	}
	if enabled != nil {
		enabledValue = boolToDB(*enabled)
	}
	result, err := db.UsersDB.ExecContext(ctx, `UPDATE mail_mailboxes
		SET quota_bytes = COALESCE(?, quota_bytes), enabled = COALESCE(?, enabled), updated_at = ?
		WHERE public_id = ?`, quotaValue, enabledValue, time.Now().UTC().Unix(), publicID)
	if err != nil {
		return fmt.Errorf("update mail user: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated mail user: %w", err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ChangeMailUserPassword updates only the stored password verifier.
func (db *DB) ChangeMailUserPassword(ctx context.Context, publicID, passwordHash string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := db.UsersDB.ExecContext(ctx, `UPDATE mail_mailboxes SET password = ?, updated_at = ? WHERE public_id = ?`, passwordHash, time.Now().UTC().Unix(), publicID)
	if err != nil {
		return fmt.Errorf("change mail user password: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check changed mail user password: %w", err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteMailUser removes a virtual mailbox only when no forwarding rules refer to it.
func (db *DB) DeleteMailUser(ctx context.Context, publicID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		var internalID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mail_mailboxes WHERE public_id = ?`, publicID).Scan(&internalID); err != nil {
			return err
		}
		for _, table := range []string{"mail_forwards", "mail_pgp_keys", "mail_sieve_scripts", "mail_archive_policies", "mail_archived_messages"} {
			var dependentRows int
			query := `SELECT COUNT(*) FROM ` + table + ` WHERE mailbox_id = ?`
			if err := tx.QueryRowContext(ctx, query, internalID).Scan(&dependentRows); err != nil {
				return fmt.Errorf("check mail user dependencies: %w", err)
			}
			if dependentRows > 0 {
				return ErrMailUserHasDependencies
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mail_mailboxes WHERE id = ?`, internalID); err != nil {
			return fmt.Errorf("delete mail user: %w", err)
		}
		return nil
	})
}

func boolToDB(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (db *DB) getMailUserByInternalID(ctx context.Context, id int64) (MailUser, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var mailUser MailUser
	var enabled int
	err := db.UsersReadDB.QueryRowContext(ctx, `SELECT m.public_id, m.local_part, d.domain, m.quota_bytes, m.enabled, m.created_at, m.updated_at
		FROM mail_mailboxes AS m JOIN mail_domains AS d ON d.id = m.domain_id WHERE m.id = ?`, id).
		Scan(&mailUser.ID, &mailUser.LocalPart, &mailUser.Domain, &mailUser.QuotaBytes, &enabled, &mailUser.CreatedAt, &mailUser.UpdatedAt)
	if err != nil {
		return MailUser{}, fmt.Errorf("get mail user: %w", err)
	}
	mailUser.Email = mailUser.LocalPart + "@" + mailUser.Domain
	mailUser.Enabled = enabled != 0
	return mailUser, nil
}

func (db *DB) backfillMailMailboxPublicIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT id FROM mail_mailboxes WHERE public_id IS NULL OR public_id = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list mailboxes missing public IDs: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read mail mailbox ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate mail mailbox IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close mail mailbox ID rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		for _, id := range ids {
			publicID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generate mail mailbox public ID: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_mailboxes SET public_id = ? WHERE id = ? AND (public_id IS NULL OR public_id = '')`, publicID.String(), id); err != nil {
				return fmt.Errorf("backfill mail mailbox public ID: %w", err)
			}
		}
		return nil
	})
}
