package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ErrLinkedTokenNotFound identifies an absent or already revoked linked token.
var ErrLinkedTokenNotFound = errors.New("linked token not found")

// ErrLinkedTokenConflict identifies a duplicate owner name or active-token limit.
var ErrLinkedTokenConflict = errors.New("linked token conflicts with owner limits or name")

// CreateLinkedToken atomically creates a linked token for an owner.
func (db *DB) CreateLinkedToken(ctx context.Context, ownerID, name, tokenHash, tokenPrefix, tokenLast4, tokenEnc string, expiresAt sql.NullInt64, maxActive int) (OwnerTokenRecord, error) {
	name, err := validateLinkedTokenName(name)
	if err != nil {
		return OwnerTokenRecord{}, err
	}
	if err := validateLinkedTokenMaterial(tokenHash, tokenPrefix, tokenLast4, tokenEnc); err != nil {
		return OwnerTokenRecord{}, err
	}
	if strings.TrimSpace(ownerID) == "" || maxActive < 1 {
		return OwnerTokenRecord{}, fmt.Errorf("linked token owner or limit is invalid")
	}
	if expiresAt.Valid && expiresAt.Int64 <= time.Now().UTC().Unix() {
		return OwnerTokenRecord{}, fmt.Errorf("linked token expiry must be in the future")
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return OwnerTokenRecord{}, fmt.Errorf("generate linked token id: %w", err)
	}
	now := time.Now().UTC().Unix()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var record OwnerTokenRecord
	err = db.Write(ctx, func(tx *sql.Tx) error {
		var ownerExists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM owners WHERE id = ?", ownerID).Scan(&ownerExists); errors.Is(err, sql.ErrNoRows) {
			return ErrLinkedTokenNotFound
		} else if err != nil {
			return fmt.Errorf("check linked token owner: %w", err)
		}
		var duplicate int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM api_tokens
			WHERE owner_id = ? AND kind = 'linked' AND name = ? LIMIT 1`, ownerID, name).Scan(&duplicate); err == nil {
			return ErrLinkedTokenConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check linked token name: %w", err)
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens
			WHERE owner_id = ? AND kind = 'linked' AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > ?)`, ownerID, now).Scan(&active); err != nil {
			return fmt.Errorf("count active linked tokens: %w", err)
		}
		if active >= maxActive {
			return ErrLinkedTokenConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO api_tokens
			(id, owner_id, kind, name, token_hash, token_prefix, token_last4, token_enc, expires_at)
			VALUES (?, ?, 'linked', ?, ?, ?, ?, ?, ?)`, tokenID.String(), ownerID, name,
			tokenHash, tokenPrefix, tokenLast4, tokenEnc, nullableInt64(expiresAt)); err != nil {
			return fmt.Errorf("insert linked token: %w", err)
		}
		record = OwnerTokenRecord{
			ID: tokenID.String(), OwnerID: ownerID, Kind: "linked", Name: sql.NullString{String: name, Valid: true},
			TokenLast4: tokenLast4, CreatedAt: now, ExpiresAt: expiresAt,
		}
		return nil
	})
	return record, err
}

// GetLinkedToken returns token metadata scoped to its owner, including expired and revoked rows.
func (db *DB) GetLinkedToken(ctx context.Context, ownerID, tokenID string) (OwnerTokenRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	record, err := scanLinkedToken(db.ServerReadDB.QueryRowContext(ctx, linkedTokenSelect+` WHERE owner_id = ? AND id = ? AND kind = 'linked'`, ownerID, tokenID))
	if errors.Is(err, sql.ErrNoRows) {
		return OwnerTokenRecord{}, ErrLinkedTokenNotFound
	}
	if err != nil {
		return OwnerTokenRecord{}, fmt.Errorf("load linked token: %w", err)
	}
	return record, nil
}

// LinkedTokenCiphertext loads a linked token's encrypted value for an explicit reveal action.
func (db *DB) LinkedTokenCiphertext(ctx context.Context, ownerID, tokenID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var encrypted string
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT token_enc FROM api_tokens
		WHERE owner_id = ? AND id = ? AND kind = 'linked'`, ownerID, tokenID).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLinkedTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load linked token ciphertext: %w", err)
	}
	return encrypted, nil
}

// RotateLinkedToken replaces a linked token's credential in place.
func (db *DB) RotateLinkedToken(ctx context.Context, ownerID, tokenID, tokenHash, tokenPrefix, tokenLast4, tokenEnc string) (OwnerTokenRecord, error) {
	if err := validateLinkedTokenMaterial(tokenHash, tokenPrefix, tokenLast4, tokenEnc); err != nil {
		return OwnerTokenRecord{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var record OwnerTokenRecord
	err := db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens
			SET token_hash = ?, token_prefix = ?, token_last4 = ?, token_enc = ?, rotated_at = ?
			WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`,
			tokenHash, tokenPrefix, tokenLast4, tokenEnc, time.Now().UTC().Unix(), ownerID, tokenID)
		if err != nil {
			return fmt.Errorf("rotate linked token: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check linked token rotation: %w", err)
		}
		if changed == 0 {
			return ErrLinkedTokenNotFound
		}
		record, err = scanLinkedToken(tx.QueryRowContext(ctx, linkedTokenSelect+` WHERE owner_id = ? AND id = ? AND kind = 'linked'`, ownerID, tokenID))
		if err != nil {
			return fmt.Errorf("read rotated linked token: %w", err)
		}
		return nil
	})
	return record, err
}

// UpdateLinkedToken changes the name and expiry without changing token identity.
func (db *DB) UpdateLinkedToken(ctx context.Context, ownerID, tokenID, name string, expiresAt sql.NullInt64) (OwnerTokenRecord, error) {
	name, err := validateLinkedTokenName(name)
	if err != nil {
		return OwnerTokenRecord{}, err
	}
	if expiresAt.Valid && expiresAt.Int64 <= time.Now().UTC().Unix() {
		return OwnerTokenRecord{}, fmt.Errorf("linked token expiry must be in the future")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var record OwnerTokenRecord
	err = db.Write(ctx, func(tx *sql.Tx) error {
		var duplicate int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM api_tokens
			WHERE owner_id = ? AND kind = 'linked' AND name = ? AND id <> ? LIMIT 1`, ownerID, name, tokenID).Scan(&duplicate); err == nil {
			return ErrLinkedTokenConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check linked token name: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET name = ?, expires_at = ?
			WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, name,
			nullableInt64(expiresAt), ownerID, tokenID)
		if err != nil {
			return fmt.Errorf("update linked token: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check linked token update: %w", err)
		}
		if changed == 0 {
			return ErrLinkedTokenNotFound
		}
		record, err = scanLinkedToken(tx.QueryRowContext(ctx, linkedTokenSelect+` WHERE owner_id = ? AND id = ? AND kind = 'linked'`, ownerID, tokenID))
		if err != nil {
			return fmt.Errorf("read updated linked token: %w", err)
		}
		return nil
	})
	return record, err
}

// RevokeLinkedToken immediately and permanently revokes one linked token.
func (db *DB) RevokeLinkedToken(ctx context.Context, ownerID, tokenID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'owner_action'
			WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, time.Now().UTC().Unix(), ownerID, tokenID)
		if err != nil {
			return fmt.Errorf("revoke linked token: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check linked token revocation: %w", err)
		}
		if changed == 0 {
			return ErrLinkedTokenNotFound
		}
		return nil
	})
}

const linkedTokenSelect = `SELECT id, owner_id, kind, name, token_prefix, token_last4, created_at, last_used_at, expires_at, revoked_at FROM api_tokens`

type linkedTokenScanner interface {
	Scan(dest ...any) error
}

func scanLinkedToken(scanner linkedTokenScanner) (OwnerTokenRecord, error) {
	var record OwnerTokenRecord
	err := scanner.Scan(&record.ID, &record.OwnerID, &record.Kind, &record.Name, &record.TokenPrefix, &record.TokenLast4,
		&record.CreatedAt, &record.LastUsedAt, &record.ExpiresAt, &record.RevokedAt)
	return record, err
}

func validateLinkedTokenName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", fmt.Errorf("linked token name must be between 1 and 64 characters")
	}
	for _, char := range name {
		if char < 0x20 || char == 0x7f {
			return "", fmt.Errorf("linked token name contains a control character")
		}
	}
	return name, nil
}

func validateLinkedTokenMaterial(tokenHash, tokenPrefix, tokenLast4, tokenEnc string) error {
	if len(tokenHash) != sha256.Size*2 || len(tokenPrefix) != 12 || !strings.HasPrefix(tokenPrefix, "own_") || len(tokenLast4) != 4 || tokenEnc == "" {
		return fmt.Errorf("linked token credential metadata is invalid")
	}
	digest, err := hex.DecodeString(tokenHash)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("linked token digest must be a SHA-256 hex string")
	}
	return nil
}

func nullableInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
