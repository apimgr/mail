package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidPGPKeyMetadata = errors.New("invalid PGP key metadata")
	pgpFingerprintPattern    = regexp.MustCompile(`^[A-F0-9]{40}$`)
)

// PGPKeyMetadata stores the public properties of the project-level keypair.
// The private and public key material remains on disk.
type PGPKeyMetadata struct {
	Fingerprint         string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	LastRotatedAt       *time.Time
	KeyserversPublished []PGPKeyserverPublication
	Revoked             bool
}

// PGPKeyserverPublication records when the current key was published to a host.
type PGPKeyserverPublication struct {
	Host        string
	PublishedAt time.Time
}

// GetPGPKeyMetadata returns the current project key metadata, or sql.ErrNoRows
// when this instance has not generated a keypair.
func (db *DB) GetPGPKeyMetadata(ctx context.Context) (PGPKeyMetadata, error) {
	if db == nil || db.ServerReadDB == nil {
		return PGPKeyMetadata{}, errors.New("server database is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var metadata PGPKeyMetadata
	var createdAt, expiresAt, published string
	var lastRotatedAt sql.NullString
	var revoked int
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked
		FROM security_pgp_key_metadata WHERE id = 1`).Scan(
		&metadata.Fingerprint, &createdAt, &expiresAt, &lastRotatedAt, &published, &revoked,
	)
	if err != nil {
		return PGPKeyMetadata{}, err
	}
	metadata.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return PGPKeyMetadata{}, fmt.Errorf("parse PGP key creation time: %w", err)
	}
	metadata.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return PGPKeyMetadata{}, fmt.Errorf("parse PGP key expiry: %w", err)
	}
	if lastRotatedAt.Valid {
		rotatedAt, parseErr := time.Parse(time.RFC3339Nano, lastRotatedAt.String)
		if parseErr != nil {
			return PGPKeyMetadata{}, fmt.Errorf("parse PGP key rotation time: %w", parseErr)
		}
		metadata.LastRotatedAt = &rotatedAt
	}
	if err := json.Unmarshal([]byte(published), &metadata.KeyserversPublished); err != nil {
		return PGPKeyMetadata{}, fmt.Errorf("decode PGP keyserver publication state: %w", err)
	}
	if metadata.KeyserversPublished == nil {
		metadata.KeyserversPublished = []PGPKeyserverPublication{}
	}
	metadata.Revoked = revoked != 0
	if err := validatePGPKeyMetadata(metadata); err != nil {
		return PGPKeyMetadata{}, fmt.Errorf("stored PGP key metadata: %w", err)
	}
	return metadata, nil
}

// SavePGPKeyMetadata validates and upserts the project-level key metadata.
func (db *DB) SavePGPKeyMetadata(ctx context.Context, metadata PGPKeyMetadata) error {
	if db == nil || db.ServerDB == nil {
		return errors.New("server database is unavailable")
	}
	if err := validatePGPKeyMetadata(metadata); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		return savePGPKeyMetadataTx(ctx, tx, metadata)
	})
}

func savePGPKeyMetadataTx(ctx context.Context, tx *sql.Tx, metadata PGPKeyMetadata) error {
	published := metadata.KeyserversPublished
	if published == nil {
		published = []PGPKeyserverPublication{}
	}
	publishedJSON, err := json.Marshal(published)
	if err != nil {
		return fmt.Errorf("encode PGP keyserver publication state: %w", err)
	}
	var lastRotatedAt any
	if metadata.LastRotatedAt != nil {
		lastRotatedAt = metadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	revoked := 0
	if metadata.Revoked {
		revoked = 1
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO security_pgp_key_metadata
		(id, fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET fingerprint = excluded.fingerprint,
			created_at = excluded.created_at, expires_at = excluded.expires_at,
			last_rotated_at = excluded.last_rotated_at,
			keyservers_published = excluded.keyservers_published, revoked = excluded.revoked`,
		metadata.Fingerprint, metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
		metadata.ExpiresAt.UTC().Format(time.RFC3339Nano), lastRotatedAt, string(publishedJSON), revoked,
	)
	if err != nil {
		return fmt.Errorf("save PGP key metadata: %w", err)
	}
	return nil
}

// RotatePGPKeyAndReports re-encrypts every PGP report and writes its metadata
// commit marker in one transaction. transform must not perform file I/O.
func (db *DB) RotatePGPKeyAndReports(ctx context.Context, metadata PGPKeyMetadata, transform func([]byte) ([]byte, error)) error {
	if db == nil || db.ServerDB == nil {
		return errors.New("server database is unavailable")
	}
	if transform == nil {
		return errors.New("PGP report transform is required")
	}
	if err := validatePGPKeyMetadata(metadata); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT tracking_id, body_enc FROM security_reports WHERE body_alg = 'pgp'`)
		if err != nil {
			return fmt.Errorf("list PGP-encrypted security reports: %w", err)
		}
		type reportCiphertext struct {
			trackingID string
			body       []byte
		}
		reports := make([]reportCiphertext, 0)
		for rows.Next() {
			var report reportCiphertext
			if err := rows.Scan(&report.trackingID, &report.body); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read PGP-encrypted security report: %w", err)
			}
			reports = append(reports, report)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate PGP-encrypted security reports: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close PGP-encrypted security reports: %w", err)
		}
		for _, report := range reports {
			body, err := transform(report.body)
			if err != nil {
				return fmt.Errorf("reencrypt security report %s: %w", report.trackingID, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE security_reports SET body_enc = ? WHERE tracking_id = ? AND body_alg = 'pgp'`, body, report.trackingID); err != nil {
				return fmt.Errorf("update re-encrypted security report %s: %w", report.trackingID, err)
			}
		}
		return savePGPKeyMetadataTx(ctx, tx, metadata)
	})
}

func validatePGPKeyMetadata(metadata PGPKeyMetadata) error {
	if !pgpFingerprintPattern.MatchString(metadata.Fingerprint) || metadata.CreatedAt.IsZero() || metadata.ExpiresAt.IsZero() || !metadata.ExpiresAt.After(metadata.CreatedAt) {
		return ErrInvalidPGPKeyMetadata
	}
	if metadata.LastRotatedAt != nil && (metadata.LastRotatedAt.IsZero() || metadata.LastRotatedAt.Before(metadata.CreatedAt)) {
		return ErrInvalidPGPKeyMetadata
	}
	for _, keyserver := range metadata.KeyserversPublished {
		if strings.TrimSpace(keyserver.Host) == "" || keyserver.PublishedAt.IsZero() {
			return ErrInvalidPGPKeyMetadata
		}
	}
	return nil
}
