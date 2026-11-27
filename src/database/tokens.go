package database

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OwnerTokenIdentity is the non-secret identity resolved from an owner token.
type OwnerTokenIdentity struct {
	OwnerID string
	Kind    string
}

// OwnerTokenRecord contains only fields safe for token-list responses.
type OwnerTokenRecord struct {
	ID          string
	OwnerID     string
	Kind        string
	Name        sql.NullString
	TokenPrefix string
	TokenLast4  string
	CreatedAt   int64
	LastUsedAt  sql.NullInt64
	ExpiresAt   sql.NullInt64
	RevokedAt   sql.NullInt64
}

// ListOwnerTokens returns token metadata without decrypting token values.
func (db *DB) ListOwnerTokens(ctx context.Context, ownerID string) ([]OwnerTokenRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, `SELECT id, owner_id, kind, name, token_prefix, token_last4, created_at,
		last_used_at, expires_at, revoked_at FROM api_tokens WHERE owner_id = ? ORDER BY kind, created_at`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list owner tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var records []OwnerTokenRecord
	for rows.Next() {
		var record OwnerTokenRecord
		if err := rows.Scan(&record.ID, &record.OwnerID, &record.Kind, &record.Name, &record.TokenPrefix, &record.TokenLast4,
			&record.CreatedAt, &record.LastUsedAt, &record.ExpiresAt, &record.RevokedAt); err != nil {
			return nil, fmt.Errorf("read owner token: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read owner tokens: %w", err)
	}
	return records, nil
}

// OwnerPrimaryTokenCiphertext loads the encrypted primary token for one owner.
func (db *DB) OwnerPrimaryTokenCiphertext(ctx context.Context, ownerID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var encrypted string
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT token_enc FROM api_tokens
		WHERE owner_id = ? AND kind = 'primary' AND revoked_at IS NULL`, ownerID).Scan(&encrypted)
	if err != nil {
		return "", fmt.Errorf("load owner primary token: %w", err)
	}
	return encrypted, nil
}

// CreateOwnerPrimaryToken creates an anonymous owner and its primary token in
// one transaction. Only the token digest and encrypted copy are persisted.
func (db *DB) CreateOwnerPrimaryToken(ctx context.Context, tokenHash, tokenPrefix, tokenLast4, tokenEnc string) (string, error) {
	if len(tokenHash) != 64 {
		return "", fmt.Errorf("token digest must be a SHA-256 hex string")
	}
	if decoded, err := hex.DecodeString(tokenHash); err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("token digest must be a SHA-256 hex string")
	}
	if len(tokenPrefix) != 12 || tokenPrefix[:4] != "own_" || len(tokenLast4) != 4 || tokenEnc == "" {
		return "", fmt.Errorf("owner token metadata is invalid")
	}
	ownerID, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate owner id: %w", err)
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = db.WithTransaction(ctx, db.ServerDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO owners (id) VALUES (?)", ownerID.String()); err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO api_tokens
			(id, owner_id, kind, token_hash, token_prefix, token_last4, token_enc)
			VALUES (?, ?, 'primary', ?, ?, ?, ?)`, tokenID.String(), ownerID.String(), tokenHash, tokenPrefix, tokenLast4, tokenEnc); err != nil {
			return fmt.Errorf("create primary token: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return ownerID.String(), nil
}

// migrateLegacyAPITokenIDs moves an old sequential key to a private legacy
// column and assigns opaque UUIDv7 identifiers without discarding token rows.
func (db *DB) migrateLegacyAPITokenIDs() error {
	if db.driver != "sqlite" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, "PRAGMA table_info(api_tokens)")
	if err != nil {
		return err
	}
	idType := ""
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "id" {
			idType = dataType
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(idType), "INTEGER") {
		return nil
	}
	tx, err := db.ServerDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "ALTER TABLE api_tokens RENAME COLUMN id TO legacy_id"); err != nil {
		return fmt.Errorf("preserve legacy token id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE api_tokens ADD COLUMN id TEXT"); err != nil {
		return fmt.Errorf("add opaque token id: %w", err)
	}
	legacyRows, err := tx.QueryContext(ctx, "SELECT legacy_id FROM api_tokens")
	if err != nil {
		return err
	}
	ids := make([]int64, 0)
	for legacyRows.Next() {
		var id int64
		if err := legacyRows.Scan(&id); err != nil {
			_ = legacyRows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := legacyRows.Err(); err != nil {
		_ = legacyRows.Close()
		return err
	}
	if err := legacyRows.Close(); err != nil {
		return err
	}
	for _, legacyID := range ids {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate opaque token id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE api_tokens SET id = ? WHERE legacy_id = ?", id.String(), legacyID); err != nil {
			return fmt.Errorf("assign opaque token id: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_public_id ON api_tokens(id)"); err != nil {
		return fmt.Errorf("index opaque token ids: %w", err)
	}
	return tx.Commit()
}

// ResolveOwnerToken returns an active owner identity after constant-time digest comparison.
func (db *DB) ResolveOwnerToken(ctx context.Context, tokenPrefix, tokenHash string) (OwnerTokenIdentity, bool, error) {
	if len(tokenPrefix) != 12 || tokenPrefix[:4] != "own_" || len(tokenHash) != 64 {
		return OwnerTokenIdentity{}, false, nil
	}
	if decoded, err := hex.DecodeString(tokenHash); err != nil || len(decoded) != 32 {
		return OwnerTokenIdentity{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, `SELECT owner_id, kind, token_hash, expires_at, revoked_at
		FROM api_tokens WHERE token_prefix = ?`, tokenPrefix)
	if err != nil {
		return OwnerTokenIdentity{}, false, fmt.Errorf("resolve owner token: %w", err)
	}
	now := time.Now().UTC().Unix()
	var matched OwnerTokenIdentity
	for rows.Next() {
		var identity OwnerTokenIdentity
		var storedHash string
		var expiresAt, revokedAt sql.NullInt64
		if err := rows.Scan(&identity.OwnerID, &identity.Kind, &storedHash, &expiresAt, &revokedAt); err != nil {
			return OwnerTokenIdentity{}, false, fmt.Errorf("read owner token: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(storedHash), []byte(tokenHash)) != 1 {
			continue
		}
		if revokedAt.Valid || (expiresAt.Valid && expiresAt.Int64 <= now) {
			continue
		}
		matched = identity
		break
	}
	rowsErr := rows.Err()
	if closeErr := rows.Close(); closeErr != nil {
		return OwnerTokenIdentity{}, false, fmt.Errorf("close owner token lookup: %w", closeErr)
	}
	if rowsErr != nil {
		return OwnerTokenIdentity{}, false, fmt.Errorf("read owner tokens: %w", rowsErr)
	}
	if matched.OwnerID == "" {
		return OwnerTokenIdentity{}, false, nil
	}
	if err := db.WithTransaction(ctx, db.ServerDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, now, tokenHash); err != nil {
			return fmt.Errorf("update owner token activity: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE owners SET last_seen_at = ? WHERE id = ?`, now, matched.OwnerID); err != nil {
			return fmt.Errorf("update owner activity: %w", err)
		}
		return nil
	}); err != nil {
		return OwnerTokenIdentity{}, false, fmt.Errorf("record owner token use: %w", err)
	}
	return matched, true, nil
}
