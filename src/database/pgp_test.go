package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
)

func TestPGPKeyMetadataRoundTripAndUpsert(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("idempotent schema setup failed: %v", err)
	}

	ctx := context.Background()
	if _, err := db.GetPGPKeyMetadata(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing metadata error = %v, want sql.ErrNoRows", err)
	}
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	expires := created.AddDate(2, 0, 0)
	metadata := PGPKeyMetadata{
		Fingerprint:         "0123456789ABCDEF0123456789ABCDEF01234567",
		CreatedAt:           created,
		ExpiresAt:           expires,
		KeyserversPublished: []PGPKeyserverPublication{{Host: "keys.openpgp.org", PublishedAt: created}, {Host: "keyserver.ubuntu.com", PublishedAt: created}},
	}
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatalf("save initial key metadata: %v", err)
	}
	got, err := db.GetPGPKeyMetadata(ctx)
	if err != nil {
		t.Fatalf("read initial key metadata: %v", err)
	}
	if got.Fingerprint != metadata.Fingerprint || !got.CreatedAt.Equal(created) || !got.ExpiresAt.Equal(expires) || got.LastRotatedAt != nil || got.Revoked || !reflect.DeepEqual(got.KeyserversPublished, metadata.KeyserversPublished) {
		t.Fatalf("initial metadata = %#v, want %#v", got, metadata)
	}

	rotated := created.AddDate(1, 0, 0)
	metadata.Fingerprint = "89ABCDEF0123456789ABCDEF0123456789ABCDEF"
	metadata.LastRotatedAt = &rotated
	metadata.KeyserversPublished = []PGPKeyserverPublication{{Host: "keys.openpgp.org", PublishedAt: created}}
	metadata.Revoked = true
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatalf("upsert rotated key metadata: %v", err)
	}
	got, err = db.GetPGPKeyMetadata(ctx)
	if err != nil {
		t.Fatalf("read rotated key metadata: %v", err)
	}
	if got.Fingerprint != metadata.Fingerprint || got.LastRotatedAt == nil || !got.LastRotatedAt.Equal(rotated) || !got.Revoked || !reflect.DeepEqual(got.KeyserversPublished, metadata.KeyserversPublished) {
		t.Fatalf("rotated metadata = %#v, want %#v", got, metadata)
	}
}

func TestSavePGPKeyMetadataRejectsInvalidValues(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	valid := PGPKeyMetadata{
		Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567",
		CreatedAt:   created,
		ExpiresAt:   created.AddDate(2, 0, 0),
	}
	for _, mutate := range []func(*PGPKeyMetadata){
		func(value *PGPKeyMetadata) { value.Fingerprint = "0123456789abcdef0123456789abcdef01234567" },
		func(value *PGPKeyMetadata) { value.Fingerprint = "short" },
		func(value *PGPKeyMetadata) { value.ExpiresAt = value.CreatedAt },
		func(value *PGPKeyMetadata) {
			rotated := value.CreatedAt.Add(-time.Second)
			value.LastRotatedAt = &rotated
		},
		func(value *PGPKeyMetadata) {
			value.KeyserversPublished = []PGPKeyserverPublication{{Host: "keys.openpgp.org", PublishedAt: created}, {Host: " ", PublishedAt: created}}
		},
	} {
		metadata := valid
		mutate(&metadata)
		if err := db.SavePGPKeyMetadata(context.Background(), metadata); !errors.Is(err, ErrInvalidPGPKeyMetadata) {
			t.Errorf("SavePGPKeyMetadata(%#v) = %v, want invalid metadata", metadata, err)
		}
	}
}

func TestRotatePGPKeyAndReportsCommitsAtomically(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	oldMetadata := PGPKeyMetadata{
		Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567",
		CreatedAt:   created, ExpiresAt: created.AddDate(2, 0, 0),
		KeyserversPublished: []PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(ctx, oldMetadata); err != nil {
		t.Fatal(err)
	}
	for id, algorithm := range map[string]string{"sec_0123456789abcdef": "pgp", "sec_abcdef0123456789": "aes-256-gcm"} {
		report := SecurityReport{
			TrackingID: id, IDWindow: 1, Severity: "high", Component: "api",
			BodyEncrypted: []byte("old:" + id), BodyAlgorithm: algorithm, ContactEncrypted: []byte("contact"),
			TokenHash: "token-hash", CreatedAt: created, UpdatedAt: created,
		}
		if algorithm == "pgp" {
			report.PGPFingerprint = oldMetadata.Fingerprint
		}
		if err := db.CreateSecurityReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	rotatedAt := created.Add(time.Hour)
	metadata := PGPKeyMetadata{
		Fingerprint: "89ABCDEF0123456789ABCDEF0123456789ABCDEF", CreatedAt: rotatedAt,
		ExpiresAt: rotatedAt.AddDate(2, 0, 0), LastRotatedAt: &rotatedAt,
		KeyserversPublished: []PGPKeyserverPublication{},
	}
	if err := db.RotatePGPKeyAndReports(ctx, metadata, func(body []byte) ([]byte, error) {
		return append([]byte("new:"), body...), nil
	}); err != nil {
		t.Fatalf("RotatePGPKeyAndReports() error = %v", err)
	}
	got, err := db.GetPGPKeyMetadata(ctx)
	if err != nil || got.Fingerprint != metadata.Fingerprint || got.LastRotatedAt == nil || !got.LastRotatedAt.Equal(rotatedAt) {
		t.Fatalf("rotation metadata = %+v, %v", got, err)
	}
	pgpReport, err := db.GetEncryptedSecurityReport(ctx, "sec_0123456789abcdef")
	if err != nil || string(pgpReport.Body) != "new:old:sec_0123456789abcdef" || pgpReport.Algorithm != "pgp" {
		t.Fatalf("rotated PGP report = %+v, %v", pgpReport, err)
	}
	aesReport, err := db.GetEncryptedSecurityReport(ctx, "sec_abcdef0123456789")
	if err != nil || string(aesReport.Body) != "old:sec_abcdef0123456789" || aesReport.Algorithm != "aes-256-gcm" {
		t.Fatalf("AES report changed during PGP rotation: %+v, %v", aesReport, err)
	}

	failedMetadata := metadata
	failedMetadata.Fingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"
	failedMetadata.CreatedAt = rotatedAt.Add(time.Minute)
	failedMetadata.ExpiresAt = failedMetadata.CreatedAt.AddDate(2, 0, 0)
	failedAt := failedMetadata.CreatedAt
	failedMetadata.LastRotatedAt = &failedAt
	if err := db.RotatePGPKeyAndReports(ctx, failedMetadata, func([]byte) ([]byte, error) {
		return nil, errors.New("decrypt failed")
	}); err == nil {
		t.Fatal("RotatePGPKeyAndReports() accepted a failed report transform")
	}
	got, err = db.GetPGPKeyMetadata(ctx)
	if err != nil || got.Fingerprint != metadata.Fingerprint {
		t.Fatalf("failed rotation changed the metadata marker: %+v, %v", got, err)
	}
	pgpReport, err = db.GetEncryptedSecurityReport(ctx, "sec_0123456789abcdef")
	if err != nil || string(pgpReport.Body) != "new:old:sec_0123456789abcdef" {
		t.Fatalf("failed rotation changed stored report: %+v, %v", pgpReport, err)
	}
}

func TestCreateSecurityReportRejectsAKeyChangedDuringEncryption(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created := time.Now().UTC().Truncate(time.Second)
	metadata := PGPKeyMetadata{
		Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567",
		CreatedAt: created, ExpiresAt: created.AddDate(2, 0, 0),
		KeyserversPublished: []PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	newReport := func(fingerprint string) SecurityReport {
		return SecurityReport{
			TrackingID: "sec_0123456789abcdef", IDWindow: 1, Severity: "high", Component: "api",
			BodyEncrypted: []byte("ciphertext"), BodyAlgorithm: "pgp", PGPFingerprint: fingerprint,
			ContactEncrypted: []byte("contact"), TokenHash: "token-hash", CreatedAt: created, UpdatedAt: created,
		}
	}
	if err := db.CreateSecurityReport(ctx, newReport("89ABCDEF0123456789ABCDEF0123456789ABCDEF")); !errors.Is(err, ErrPGPKeyChanged) {
		t.Fatalf("stale PGP key report error = %v, want ErrPGPKeyChanged", err)
	}
	if err := db.CreateSecurityReport(ctx, newReport(metadata.Fingerprint)); err != nil {
		t.Fatalf("current-key report error = %v", err)
	}
	metadata.Revoked = true
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSecurityReport(ctx, newReport(metadata.Fingerprint)); !errors.Is(err, ErrPGPKeyChanged) {
		t.Fatalf("revoked PGP key report error = %v, want ErrPGPKeyChanged", err)
	}
}
