package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MailAlias is the public mail-alias record; internal numeric IDs are never exposed.
type MailAlias struct {
	ID          string
	Source      string
	Destination string
	Domain      string
	Enabled     bool
	CreatedAt   int64
}

// ListMailAliases returns aliases with opaque identifiers and domain names.
func (db *DB) ListMailAliases(ctx context.Context, page, limit int) ([]MailAlias, int64, error) {
	if page < 1 || limit < 1 || limit > 250 {
		return nil, 0, fmt.Errorf("mail alias pagination is outside the allowed range")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const joins = ` FROM mail_aliases AS a JOIN mail_domains AS d ON d.id = a.domain_id`
	var total int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT COUNT(*)`+joins).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count mail aliases: %w", err)
	}
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT a.public_id, a.source, a.destination, d.domain, a.enabled, a.created_at`+joins+` ORDER BY a.source, a.id LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list mail aliases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	aliases := make([]MailAlias, 0)
	for rows.Next() {
		var alias MailAlias
		var enabled int
		if err := rows.Scan(&alias.ID, &alias.Source, &alias.Destination, &alias.Domain, &enabled, &alias.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("read mail alias: %w", err)
		}
		alias.Enabled = enabled != 0
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mail aliases: %w", err)
	}
	return aliases, total, nil
}

// CreateMailAlias persists a system-managed alias and returns its public record.
func (db *DB) CreateMailAlias(ctx context.Context, domain, source, destination string) (MailAlias, error) {
	publicID, err := uuid.NewV7()
	if err != nil {
		return MailAlias{}, fmt.Errorf("generate mail alias public ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var aliasID int64
	err = db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mail_domains WHERE domain = ?`, domain).Scan(&aliasID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mail_aliases (public_id, domain_id, owner_type, owner_id, source, destination, enabled, created_at)
			VALUES (?, ?, 'system', 'system', ?, ?, 1, ?)`, publicID.String(), aliasID, source, destination, time.Now().UTC().Unix())
		if err != nil {
			return fmt.Errorf("insert mail alias: %w", err)
		}
		aliasID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return MailAlias{}, fmt.Errorf("create mail alias: %w", err)
	}
	return db.getMailAliasByInternalID(ctx, aliasID)
}

// DeleteMailAlias removes one alias selected by its opaque public identifier.
func (db *DB) DeleteMailAlias(ctx context.Context, publicID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM mail_aliases WHERE public_id = ?`, publicID)
		if err != nil {
			return fmt.Errorf("delete mail alias: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check deleted mail alias: %w", err)
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (db *DB) getMailAliasByInternalID(ctx context.Context, id int64) (MailAlias, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var alias MailAlias
	var enabled int
	err := db.UsersReadDB.QueryRowContext(ctx, `SELECT a.public_id, a.source, a.destination, d.domain, a.enabled, a.created_at
		FROM mail_aliases AS a JOIN mail_domains AS d ON d.id = a.domain_id WHERE a.id = ?`, id).
		Scan(&alias.ID, &alias.Source, &alias.Destination, &alias.Domain, &enabled, &alias.CreatedAt)
	if err != nil {
		return MailAlias{}, fmt.Errorf("get mail alias: %w", err)
	}
	alias.Enabled = enabled != 0
	return alias, nil
}

func (db *DB) backfillMailAliasPublicIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT id FROM mail_aliases WHERE public_id IS NULL OR public_id = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list aliases missing public IDs: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read mail alias ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate mail alias IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close mail alias ID rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		for _, id := range ids {
			publicID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generate mail alias public ID: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_aliases SET public_id = ? WHERE id = ? AND (public_id IS NULL OR public_id = '')`, publicID.String(), id); err != nil {
				return fmt.Errorf("backfill mail alias public ID: %w", err)
			}
		}
		return nil
	})
}
