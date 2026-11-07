package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/apimgr/mail/config"
)

func TestSQLiteSchemaAndTransactionLifecycle(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.ServerDB == db.UsersDB {
		t.Fatal("SQLite server and users databases should be separate")
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("schema application is not idempotent: %v", err)
	}
	var version int
	if err := db.ServerDB.QueryRow("SELECT version FROM config_meta WHERE id = 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO config (key, value) VALUES ('test', 'one')",
		"UPDATE config SET value = 'two' WHERE key = 'test'",
		"DELETE FROM config WHERE key = 'test'",
	} {
		if _, err := db.ServerDB.Exec(statement); err != nil {
			t.Fatal(err)
		}
		version++
		var got int
		if err := db.ServerDB.QueryRow("SELECT version FROM config_meta WHERE id = 1").Scan(&got); err != nil || got != version {
			t.Fatalf("config version = %d, want %d (err=%v)", got, version, err)
		}
	}
	if err := db.WithTransaction(context.Background(), db.ServerDB, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	rollbackErr := errors.New("rollback")
	if err := db.WithTransaction(context.Background(), db.ServerDB, func(*sql.Tx) error { return rollbackErr }); !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback transaction error = %v", err)
	}
}

func TestOpenDBAndSerializableRetryErrors(t *testing.T) {
	if _, err := openDB("unused", "not-a-registered-driver"); err == nil {
		t.Fatal("openDB accepted an unknown driver")
	}
	db := &DB{}
	if err := db.WithSerializableRetry(context.Background(), nil, 0, func(_ *sql.Tx) error { return nil }); err == nil || err.Error() != "max retries exceeded" {
		t.Fatalf("zero-attempt retry error = %v", err)
	}
}
