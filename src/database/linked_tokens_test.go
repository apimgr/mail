package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
)

func TestLinkedTokenLifecycle(t *testing.T) {
	db := newLinkedTokenTestDB(t)
	ctx := context.Background()
	ownerID := createLinkedTokenTestOwner(t, db, "owner-one")
	primary, err := db.CreateLinkedToken(ctx, ownerID, "ci", linkedTokenHash("ci-v1"), "own_abcdefgh", "1234", "encrypted-v1", sql.NullInt64{}, 2)
	if err != nil {
		t.Fatalf("create linked token: %v", err)
	}
	if primary.Kind != "linked" || primary.Name.String != "ci" || primary.ID == "" || primary.TokenLast4 != "1234" {
		t.Fatalf("created token metadata = %#v", primary)
	}

	if _, err := db.CreateLinkedToken(ctx, ownerID, "ci", linkedTokenHash("ci-v2"), "own_bcdefghi", "2345", "encrypted-v2", sql.NullInt64{}, 2); !errors.Is(err, ErrLinkedTokenConflict) {
		t.Fatalf("duplicate token name error = %v, want conflict", err)
	}
	other, err := db.CreateLinkedToken(ctx, ownerID, "laptop", linkedTokenHash("laptop-v1"), "own_cdefghij", "3456", "encrypted-v3", sql.NullInt64{}, 2)
	if err != nil {
		t.Fatalf("create second linked token: %v", err)
	}
	if _, err := db.CreateLinkedToken(ctx, ownerID, "automation", linkedTokenHash("automation-v1"), "own_defghijk", "4567", "encrypted-v4", sql.NullInt64{}, 2); !errors.Is(err, ErrLinkedTokenConflict) {
		t.Fatalf("max-active token error = %v, want conflict", err)
	}
	if _, err := db.GetLinkedToken(ctx, "different-owner", primary.ID); !errors.Is(err, ErrLinkedTokenNotFound) {
		t.Fatalf("cross-owner token lookup error = %v, want not found", err)
	}

	ciphertext, err := db.LinkedTokenCiphertext(ctx, ownerID, primary.ID)
	if err != nil || ciphertext != "encrypted-v1" {
		t.Fatalf("linked token ciphertext = %q, err=%v", ciphertext, err)
	}
	rotated, err := db.RotateLinkedToken(ctx, ownerID, primary.ID, linkedTokenHash("ci-v2"), "own_efghijkl", "5678", "encrypted-rotated")
	if err != nil || rotated.ID != primary.ID || rotated.Name.String != primary.Name.String || rotated.ExpiresAt.Valid {
		t.Fatalf("rotated token = %#v, err=%v", rotated, err)
	}
	var storedHash string
	if err := db.ServerReadDB.QueryRowContext(ctx, "SELECT token_hash FROM api_tokens WHERE owner_id = ? AND id = ?", ownerID, primary.ID).Scan(&storedHash); err != nil || storedHash != linkedTokenHash("ci-v2") {
		t.Fatalf("rotated token hash = %q, err=%v", storedHash, err)
	}

	expires := sql.NullInt64{Int64: time.Now().UTC().Add(24 * time.Hour).Unix(), Valid: true}
	updated, err := db.UpdateLinkedToken(ctx, ownerID, other.ID, "laptop-cli", expires)
	if err != nil || updated.Name.String != "laptop-cli" || updated.ExpiresAt.Int64 != expires.Int64 {
		t.Fatalf("updated token = %#v, err=%v", updated, err)
	}
	if _, err := db.UpdateLinkedToken(ctx, ownerID, other.ID, "ci", sql.NullInt64{}); !errors.Is(err, ErrLinkedTokenConflict) {
		t.Fatalf("update duplicate token name error = %v, want conflict", err)
	}
	if err := db.RevokeLinkedToken(ctx, ownerID, primary.ID); err != nil {
		t.Fatalf("revoke linked token: %v", err)
	}
	revoked, err := db.GetLinkedToken(ctx, ownerID, primary.ID)
	if err != nil || !revoked.RevokedAt.Valid {
		t.Fatalf("revoked token = %#v, err=%v", revoked, err)
	}
	if err := db.RevokeLinkedToken(ctx, ownerID, primary.ID); !errors.Is(err, ErrLinkedTokenNotFound) {
		t.Fatalf("repeat revoke error = %v, want not found", err)
	}
	if _, err := db.RotateLinkedToken(ctx, ownerID, primary.ID, linkedTokenHash("ci-v3"), "own_fghijklm", "6789", "encrypted-v5"); !errors.Is(err, ErrLinkedTokenNotFound) {
		t.Fatalf("rotate revoked token error = %v, want not found", err)
	}
}

func TestLinkedTokenCreateIgnoresExpiredTokensForLimit(t *testing.T) {
	db := newLinkedTokenTestDB(t)
	ctx := context.Background()
	ownerID := createLinkedTokenTestOwner(t, db, "owner-expiry")
	token, err := db.CreateLinkedToken(ctx, ownerID, "short-lived", linkedTokenHash("short-v1"), "own_abcdefgh", "1234", "encrypted-v1", sql.NullInt64{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(ctx, "UPDATE api_tokens SET expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Hour).Unix(), token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLinkedToken(ctx, ownerID, "replacement", linkedTokenHash("replacement-v1"), "own_bcdefghi", "2345", "encrypted-v2", sql.NullInt64{}, 1); err != nil {
		t.Fatalf("expired token counted toward active limit: %v", err)
	}
}

func TestLinkedTokenValidation(t *testing.T) {
	db := newLinkedTokenTestDB(t)
	ownerID := createLinkedTokenTestOwner(t, db, "owner-validation")
	for _, name := range []string{"", "  ", strings.Repeat("x", 65), "bad\nname"} {
		if _, err := db.CreateLinkedToken(context.Background(), ownerID, name, linkedTokenHash(name), "own_abcdefgh", "1234", "encrypted", sql.NullInt64{}, 25); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	if _, err := db.CreateLinkedToken(context.Background(), "missing-owner", "valid", linkedTokenHash("valid"), "own_abcdefgh", "1234", "encrypted", sql.NullInt64{}, 25); !errors.Is(err, ErrLinkedTokenNotFound) {
		t.Fatalf("missing owner error = %v, want not found", err)
	}
}

func newLinkedTokenTestDB(t *testing.T) *DB {
	t.Helper()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatalf("connect linked token database: %v", err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("initialize linked token database schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close linked token database: %v", err)
		}
	})
	return db
}

func createLinkedTokenTestOwner(t *testing.T, db *DB, value string) string {
	t.Helper()
	ownerID, err := db.CreateOwnerPrimaryToken(context.Background(), linkedTokenHash(value), "own_abcdefgh", "1234", "encrypted-primary")
	if err != nil {
		t.Fatalf("create linked token test owner: %v", err)
	}
	return ownerID
}

func linkedTokenHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
