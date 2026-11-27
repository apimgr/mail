package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

var generatedSecretNames = []string{"installation_secret", "cookie_signing_key", "csrf_token_secret"}

// EnsureGeneratedSecrets creates missing application secrets without replacing existing values.
func (db *DB) EnsureGeneratedSecrets(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		for _, name := range generatedSecretNames {
			value := make([]byte, 32)
			if _, err := rand.Read(value); err != nil {
				return fmt.Errorf("generate %s: %w", name, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO app_secrets (name, value, updated_at)
				VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`, name,
				base64.StdEncoding.EncodeToString(value), time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				return fmt.Errorf("initialize %s: %w", name, err)
			}
		}
		return nil
	})
}

// GeneratedSecret reads a generated application secret without exposing the backing table.
func (db *DB) GeneratedSecret(ctx context.Context, name string) ([]byte, error) {
	if !validGeneratedSecretName(name) {
		return nil, fmt.Errorf("unknown generated secret")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var value string
	if err := db.ServerReadDB.QueryRowContext(ctx, `SELECT value FROM app_secrets WHERE name = ?`, name).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("generated secret %q is missing", name)
		}
		return nil, fmt.Errorf("read generated secret: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("generated secret %q is invalid", name)
	}
	return decoded, nil
}

func validGeneratedSecretName(name string) bool {
	for _, candidate := range generatedSecretNames {
		if name == candidate {
			return true
		}
	}
	return false
}
