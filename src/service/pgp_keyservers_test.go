package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestPGPKeyserverRetryAndPublicationState(t *testing.T) {
	db, securityDir, metadata := newPGPKeyserverFixture(t)
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	attempts := 0
	submit := func(_ context.Context, host string, public []byte, fingerprint string) error {
		attempts++
		if host != "keys.openpgp.org" || len(public) == 0 || fingerprint != metadata.Fingerprint {
			t.Errorf("publisher arguments = %q, %d bytes, %q", host, len(public), fingerprint)
		}
		if attempts == 1 {
			return &security.PGPKeyserverError{StatusCode: 503, Retryable: true}
		}
		return nil
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now, false, submit); err == nil {
		t.Fatal("initial publication error = nil, want retryable failure")
	}
	statePath := filepath.Join(securityDir, "keyservers.state")
	state, err := loadPGPKeyserverState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Entries["keys.openpgp.org"]
	if entry.Attempts != 1 || entry.NextAttempt == nil || !entry.NextAttempt.Equal(now.Add(time.Minute)) {
		t.Fatalf("retry state after initial failure = %#v", entry)
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now.Add(30*time.Second), true, submit); err != nil {
		t.Fatalf("early retry = %v", err)
	}
	if attempts != 1 {
		t.Fatalf("early retry attempts = %d, want 1", attempts)
	}
	publishedAt := now.Add(time.Minute)
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, publishedAt, true, submit); err != nil {
		t.Fatalf("due retry = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("due retry attempts = %d, want 2", attempts)
	}
	metadata, err = db.GetPGPKeyMetadata(context.Background())
	if err != nil || len(metadata.KeyserversPublished) != 1 || metadata.KeyserversPublished[0].Host != "keys.openpgp.org" || !metadata.KeyserversPublished[0].PublishedAt.Equal(publishedAt) {
		t.Fatalf("published key metadata = %#v, error %v", metadata, err)
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, publishedAt.Add(time.Hour), false, submit); err != nil {
		t.Fatalf("duplicate publication = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("duplicate publication attempts = %d, want 2", attempts)
	}
	info, err := os.Stat(statePath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("keyserver state permissions = %v, error %v", info, err)
	}
}

func TestPGPKeyserverFinalFailuresAreNotRetried(t *testing.T) {
	db, securityDir, _ := newPGPKeyserverFixture(t)
	now := time.Now().UTC()
	attempts := 0
	submit := func(context.Context, string, []byte, string) error {
		attempts++
		return &security.PGPKeyserverError{StatusCode: 400, Retryable: false}
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now, false, submit); err == nil {
		t.Fatal("permanent publication failure = nil")
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now, true, submit); err != nil {
		t.Fatalf("retry of permanent failure = %v, want no retry", err)
	}
	state, err := loadPGPKeyserverState(filepath.Join(securityDir, "keyservers.state"))
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Entries["keys.openpgp.org"]
	if attempts != 1 || entry.Attempts != maxPGPKeyserverAttempts || entry.NextAttempt != nil {
		t.Fatalf("permanent failure state = %#v, calls %d", entry, attempts)
	}
}

func TestPGPKeyserverBackoffCapsAndStopsAfterEightAttempts(t *testing.T) {
	db, securityDir, metadata := newPGPKeyserverFixture(t)
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	statePath := filepath.Join(securityDir, "keyservers.state")
	state := pgpKeyserverState{Version: 1, Entries: map[string]pgpKeyserverStateEntry{
		"keys.openpgp.org": {Fingerprint: metadata.Fingerprint, Attempts: 6, NextAttempt: &now},
	}}
	if err := savePGPKeyserverState(statePath, state); err != nil {
		t.Fatal(err)
	}
	submit := func(context.Context, string, []byte, string) error {
		return &security.PGPKeyserverError{StatusCode: 502, Retryable: true}
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now, true, submit); err == nil {
		t.Fatal("seventh retry error = nil")
	}
	state, err := loadPGPKeyserverState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Entries["keys.openpgp.org"]
	if entry.Attempts != 7 || entry.NextAttempt == nil || !entry.NextAttempt.Equal(now.Add(64*time.Minute)) {
		t.Fatalf("seventh-attempt state = %#v", entry)
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, *entry.NextAttempt, true, submit); err == nil {
		t.Fatal("eighth retry error = nil")
	}
	state, err = loadPGPKeyserverState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry = state.Entries["keys.openpgp.org"]
	if entry.Attempts != maxPGPKeyserverAttempts || entry.NextAttempt != nil {
		t.Fatalf("eighth-attempt state = %#v", entry)
	}
}

func TestPGPKeyserverStateReconcilesPublishedHostIntoMetadata(t *testing.T) {
	db, securityDir, metadata := newPGPKeyserverFixture(t)
	publishedAt := time.Now().UTC().Truncate(time.Second)
	if err := savePGPKeyserverState(filepath.Join(securityDir, "keyservers.state"), pgpKeyserverState{
		Version: 1,
		Entries: map[string]pgpKeyserverStateEntry{
			"keys.openpgp.org": {Fingerprint: metadata.Fingerprint, PublishedAt: &publishedAt},
		},
	}); err != nil {
		t.Fatal(err)
	}
	submit := func(context.Context, string, []byte, string) error {
		t.Fatal("published host was submitted again")
		return nil
	}
	if err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, publishedAt, false, submit); err != nil {
		t.Fatalf("state reconciliation error = %v", err)
	}
	metadata, err := db.GetPGPKeyMetadata(context.Background())
	if err != nil || len(metadata.KeyserversPublished) != 1 || !metadata.KeyserversPublished[0].PublishedAt.Equal(publishedAt) {
		t.Fatalf("reconciled metadata = %#v, error %v", metadata, err)
	}
}

func TestLoadSavePGPKeyserverStateValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security", "keyservers.state")
	state, err := loadPGPKeyserverState(path)
	if err != nil || state.Version != 1 || len(state.Entries) != 0 {
		t.Fatalf("missing state = %#v, error %v", state, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := savePGPKeyserverState(path, pgpKeyserverState{Version: 1, Entries: map[string]pgpKeyserverStateEntry{"host": {Fingerprint: "fingerprint", Attempts: 2}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPGPKeyserverState(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPGPKeyserverState(path); err == nil {
		t.Fatal("malformed state error = nil")
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPGPKeyserverState(path); err == nil {
		t.Fatal("unsupported state version error = nil")
	}
}

func TestPublishPGPKeyserversRejectsUnavailableAndInvalidKeys(t *testing.T) {
	now := time.Now().UTC()
	if err := publishPGPKeyservers(context.Background(), nil, "", nil, now, false, nil); err == nil {
		t.Fatal("publish with missing database succeeded")
	}
	db, securityDir, metadata := newPGPKeyserverFixture(t)
	if err := publishPGPKeyservers(context.Background(), db, securityDir, nil, now, false, nil); err == nil {
		t.Fatal("publish with missing submit function succeeded")
	}
	for _, test := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{name: "missing public key", setup: func(t *testing.T) {
			if err := os.Remove(filepath.Join(securityDir, "pgp.pub.asc")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mismatched public key", setup: func(t *testing.T) {
			publicKey, _, _, _, err := security.GeneratePGPKey("Other Security <other@example.test>", now)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), publicKey, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "invalid state", setup: func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(securityDir, "keyservers.state"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "revoked key", setup: func(t *testing.T) {
			metadata.Revoked = true
			if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "expired key", setup: func(t *testing.T) {
			metadata.ExpiresAt = now.Add(-time.Minute)
			metadata.CreatedAt = metadata.ExpiresAt.AddDate(-2, 0, 0)
			if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, securityDir, metadata = newPGPKeyserverFixture(t)
			test.setup(t)
			err := publishPGPKeyservers(context.Background(), db, securityDir, []string{"keys.openpgp.org"}, now, false, func(context.Context, string, []byte, string) error { return nil })
			if err == nil {
				t.Fatal("publishPGPKeyservers() error = nil")
			}
		})
	}
}

func newPGPKeyserverFixture(t *testing.T) (*database.DB, string, database.PGPKeyMetadata) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	created := time.Now().UTC().Truncate(time.Second)
	publicKey, _, fingerprint, expires, err := security.GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	securityDir := filepath.Join(root, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), publicKey, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := database.PGPKeyMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: expires, KeyserversPublished: []database.PGPKeyserverPublication{}}
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	return db, securityDir, metadata
}
