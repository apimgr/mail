package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/google/uuid"
)

func TestSQLiteSchemaAndTransactionLifecycle(t *testing.T) {
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
	if db.ServerDB != db.UsersDB || db.ServerReadDB != db.UsersReadDB {
		t.Fatal("SQLite server state must use one database file with dedicated read and write handles")
	}
	if _, err := db.ServerReadDB.Exec("CREATE TABLE read_handle_write_test (id INTEGER)"); err == nil {
		t.Fatal("SQLite read handle accepted a schema write")
	}
	if got := db.ServerReadDB.Stats().MaxOpenConnections; got != 25 {
		t.Fatalf("SQLite read pool max open = %d, want 25", got)
	}
	var foreignKeys int
	if err := db.ServerDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("SQLite foreign_keys pragma = %d (err=%v)", foreignKeys, err)
	}
	var journalMode string
	if err := db.ServerDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("SQLite journal_mode pragma = %q (err=%v)", journalMode, err)
	}
	var busyTimeout int
	if err := db.ServerDB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 10000 {
		t.Fatalf("SQLite busy_timeout pragma = %d (err=%v)", busyTimeout, err)
	}
	if _, err := db.ServerDB.Exec(`CREATE TABLE rate_limits (
		key TEXT PRIMARY KEY, count INTEGER NOT NULL DEFAULT 1,
		window_start INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create legacy rate limit table: %v", err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendAuditEvent(context.Background(), AuditEvent{
		Category: "service", Action: "operation", ActorType: "operator", ActorID: "admin",
		ActorIP: "192.0.2.1", TargetType: "mail_service", TargetID: "postfix",
		Details: map[string]any{"action": "restart"}, Success: true,
	}); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
	var category, action, actorType, actorID, actorIP, targetType, targetID, details string
	var success int
	if err := db.ServerReadDB.QueryRow(`SELECT category, action, actor_type, actor_id, actor_ip,
		target_type, target_id, details, success FROM audit_log WHERE target_id = ?`, "postfix").Scan(
		&category, &action, &actorType, &actorID, &actorIP, &targetType, &targetID, &details, &success); err != nil {
		t.Fatalf("read appended audit event: %v", err)
	}
	if category != "service" || action != "operation" || actorType != "operator" || actorID != "admin" || actorIP != "192.0.2.1" || targetType != "mail_service" || targetID != "postfix" || details != `{"action":"restart"}` || success != 1 {
		t.Fatalf("audit event was not persisted accurately: %s/%s %s %s %s %s %s %s %d", category, action, actorType, actorID, actorIP, targetType, targetID, details, success)
	}
	if found, err := db.HasAuditEvent(context.Background(), "service", "operation", "mail_service", "postfix"); err != nil || !found {
		t.Fatalf("HasAuditEvent() for persisted event = %v, %v", found, err)
	}
	if found, err := db.HasAuditEvent(context.Background(), "service", "operation", "mail_service", "dovecot"); err != nil || found {
		t.Fatalf("HasAuditEvent() for missing event = %v, %v", found, err)
	}
	if err := db.AppendAuditEvent(context.Background(), AuditEvent{Action: "missing-category"}); err == nil {
		t.Fatal("audit event without a category was accepted")
	}
	if !sqliteColumnExists(t, db.ServerDB, "rate_limits", "prev_count") {
		t.Fatal("rate_limits migration did not add prev_count")
	}
	now := time.Now().UTC().Truncate(time.Hour)
	if err := db.FlushRequestStats(context.Background(), 3, map[int64]int64{now.Unix(): 3}, now); err != nil {
		t.Fatalf("flush request stats: %v", err)
	}
	stats, err := db.LoadRequestStats(context.Background(), now)
	if err != nil || stats.Total != 3 || stats.Hourly[now.Unix()] != 3 {
		t.Fatalf("request stats = %#v (err=%v)", stats, err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("schema application is not idempotent: %v", err)
	}
	secret, err := db.GeneratedSecret(context.Background(), "installation_secret")
	if err != nil || len(secret) != 32 {
		t.Fatalf("generated installation secret length = %d (err=%v)", len(secret), err)
	}
	if err := db.EnsureGeneratedSecrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	secretAgain, err := db.GeneratedSecret(context.Background(), "installation_secret")
	if err != nil || string(secretAgain) != string(secret) {
		t.Fatalf("generated secret changed during initialization (err=%v)", err)
	}
	for _, table := range []string{"owners", "api_tokens"} {
		var count int
		if err := db.ServerDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("token schema table %q count = %d (err=%v)", table, count, err)
		}
	}
	var systemOwnerCount int
	if err := db.ServerReadDB.QueryRow("SELECT count(*) FROM owners WHERE id = 'system'").Scan(&systemOwnerCount); err != nil || systemOwnerCount != 1 {
		t.Fatalf("system owner count = %d (err=%v), want 1", systemOwnerCount, err)
	}
	for _, table := range []string{"mail_domains", "mail_mailboxes", "mail_aliases", "mail_forwards", "mail_dkim_keys", "mail_queue_log"} {
		var count int
		if err := db.ServerDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("mail schema table %q count = %d (err=%v)", table, count, err)
		}
	}
	testOwnerPrimaryTokenPersistence(t, db)
	for _, table := range []string{"config", "config_meta"} {
		var count int
		if err := db.ServerDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("configuration table %q count = %d (err=%v); configuration belongs in files", table, count, err)
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

func TestLibSQLRequiresRemoteURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "libsql"
	if _, err := Connect(cfg); err == nil || !strings.Contains(err.Error(), "requires a remote database URL") {
		t.Fatalf("Connect without libSQL URL error = %v", err)
	}
	for _, driver := range sql.Drivers() {
		if driver == "libsql" {
			return
		}
	}
	t.Fatalf("registered database drivers = %v, want libsql", sql.Drivers())
}

func TestListSecurityReportSummaries(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "security.db")
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
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.CreateSecurityReport(context.Background(), SecurityReport{
		TrackingID: "sec_0123456789abcdef", IDWindow: 1, Severity: "high", Component: "api",
		BodyEncrypted: []byte("ciphertext"), BodyAlgorithm: "aes-256-gcm", ContactEncrypted: []byte("contact"),
		TokenHash: strings.Repeat("a", 64), CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	encrypted, err := db.GetEncryptedSecurityReport(context.Background(), "sec_0123456789abcdef")
	if err != nil || encrypted.Algorithm != "aes-256-gcm" || string(encrypted.Body) != "ciphertext" {
		t.Fatalf("encrypted report export = %#v, err %v", encrypted, err)
	}
	if _, err := db.GetEncryptedSecurityReport(context.Background(), "sec_ffffffffffffffff"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing encrypted report error = %v, want sql.ErrNoRows", err)
	}
	items, err := db.ListSecurityReportSummaries(context.Background(), "received")
	if err != nil || len(items) != 1 {
		t.Fatalf("ListSecurityReportSummaries() = %#v, %v", items, err)
	}
	item := items[0]
	if item.TrackingID != "sec_0123456789abcdef" || item.Status != "received" || item.Severity != "high" || item.Component != "api" || !item.CreatedAt.Equal(created) || !item.UpdatedAt.Equal(created) {
		t.Fatalf("unexpected security report summary: %#v", item)
	}
	items, err = db.ListSecurityReportSummaries(context.Background(), "closed")
	if err != nil || len(items) != 0 {
		t.Fatalf("filtered summaries = %#v, %v; want empty", items, err)
	}
	if _, err := db.ServerDB.Exec(`UPDATE security_reports SET researcher_comment = ?, disclosure_date = ? WHERE tracking_id = ?`, "We are investigating", "2026-03-01", "sec_0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	details, err := db.GetSecurityReportDetails(context.Background(), "sec_0123456789abcdef")
	if err != nil || details.ResearcherComment != "We are investigating" || !details.DisclosureDate.Valid || details.DisclosureDate.String != "2026-03-01" {
		t.Fatalf("GetSecurityReportDetails() = %#v, %v", details, err)
	}
	if _, err := db.GetSecurityReportDetails(context.Background(), "sec_ffffffffffffffff"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing report error = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteClosedSecurityReportsHonorsRetentionAndPreservesOpenReports(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "retention.db")
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
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"sec_aaaaaaaaaaaaaaaa", "sec_bbbbbbbbbbbbbbbb", "sec_cccccccccccccccc"} {
		if err := db.CreateSecurityReport(context.Background(), SecurityReport{
			TrackingID: id, IDWindow: 1, Severity: "low", Component: "api", BodyEncrypted: []byte("ciphertext"),
			BodyAlgorithm: "aes-256-gcm", ContactEncrypted: []byte("contact"), TokenHash: strings.Repeat("b", 64),
			CreatedAt: now.Add(-400 * 24 * time.Hour), UpdatedAt: now.Add(-400 * 24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	oldClosed := now.AddDate(0, 0, -366).Format(time.RFC3339Nano)
	recentClosed := now.AddDate(0, 0, -364).Format(time.RFC3339Nano)
	if _, err := db.ServerDB.Exec(`UPDATE security_reports SET closed_at = ? WHERE tracking_id = ?`, oldClosed, "sec_aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.Exec(`UPDATE security_reports SET closed_at = ? WHERE tracking_id = ?`, recentClosed, "sec_bbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	removed, err := db.DeleteClosedSecurityReports(context.Background(), 365, now)
	if err != nil || removed != 1 {
		t.Fatalf("DeleteClosedSecurityReports() = %d, %v; want 1 removal", removed, err)
	}
	var remaining int
	if err := db.ServerReadDB.QueryRow(`SELECT COUNT(*) FROM security_reports`).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("remaining reports = %d, %v; want recent and open reports", remaining, err)
	}
	if _, err := db.DeleteClosedSecurityReports(context.Background(), 0, now); err == nil {
		t.Fatal("zero-day report retention was accepted")
	}
}

func TestUpdateSecurityReportTransitionsAndDisclosureCredit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "security-update.db")
	db, err := Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.CreateSecurityReport(context.Background(), SecurityReport{
		TrackingID: "sec_0123456789abcdef", IDWindow: 1, Severity: "medium", Component: "api",
		BodyEncrypted: []byte("ciphertext"), BodyAlgorithm: "aes-256-gcm", ContactEncrypted: []byte("contact"),
		CreditPreference: "anon", TokenHash: strings.Repeat("a", 64), CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	oldStatus, err := db.UpdateSecurityReport(context.Background(), "sec_0123456789abcdef", SecurityReportUpdate{
		Status: "disclosed", Comment: "The fixed version is available.", DisclosureDate: "2026-02-03", Severity: "high",
	}, now)
	if err != nil || oldStatus != "received" {
		t.Fatalf("UpdateSecurityReport() = old status %q, err %v", oldStatus, err)
	}
	details, err := db.GetSecurityReportDetails(context.Background(), "sec_0123456789abcdef")
	if err != nil || details.Status != "disclosed" || details.Severity != "high" || details.ResearcherComment != "The fixed version is available." || !details.DisclosureDate.Valid || details.DisclosureDate.String != "2026-02-03" {
		t.Fatalf("updated report details = %#v, err %v", details, err)
	}
	thanks, err := db.ListSecurityThanks(context.Background())
	if err != nil || len(thanks) != 1 || thanks[0].Credit != "Anonymous Researcher #1" || thanks[0].Severity != "high" || thanks[0].DisclosedOn != "2026-02-03" {
		t.Fatalf("disclosure credit = %#v, err %v", thanks, err)
	}
	if _, err := db.UpdateSecurityReport(context.Background(), "sec_0123456789abcdef", SecurityReportUpdate{Status: "confirmed"}, now.Add(time.Hour)); !errors.Is(err, ErrInvalidSecurityReportUpdate) {
		t.Fatalf("backward status transition error = %v, want invalid update", err)
	}
	if _, err := db.UpdateSecurityReport(context.Background(), "sec_0123456789abcdef", SecurityReportUpdate{Comment: ""}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	details, err = db.GetSecurityReportDetails(context.Background(), "sec_0123456789abcdef")
	if err != nil || details.ResearcherComment != "The fixed version is available." {
		t.Fatalf("empty partial update erased comment: %#v, err %v", details, err)
	}
}

func TestSecurityReportUpdateValidationAndTransitions(t *testing.T) {
	for _, invalid := range []SecurityReportUpdate{
		{Status: "unknown"},
		{Severity: "urgent"},
		{Comment: strings.Repeat("x", 2001)},
		{DisclosureDate: "2026-02-30"},
	} {
		if err := validateSecurityReportUpdate(invalid); !errors.Is(err, ErrInvalidSecurityReportUpdate) {
			t.Errorf("validateSecurityReportUpdate(%#v) = %v, want invalid update", invalid, err)
		}
	}
	if err := validateSecurityReportUpdate(SecurityReportUpdate{Status: "triaged", Severity: "critical", Comment: "Investigating", DisclosureDate: "2026-02-28"}); err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	for _, pair := range [][2]string{{"received", "triaged"}, {"triaged", "confirmed"}, {"confirmed", "patching"}, {"patching", "disclosed"}, {"received", "wont_fix"}} {
		if !securityReportStatusCanAdvance(pair[0], pair[1]) {
			t.Errorf("status %q could not advance to %q", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"triaged", "received"}, {"disclosed", "patching"}, {"wont_fix", "received"}, {"unknown", "triaged"}} {
		if securityReportStatusCanAdvance(pair[0], pair[1]) {
			t.Errorf("status %q unexpectedly advanced to %q", pair[0], pair[1])
		}
	}
	if !securityReportStatusCanAdvance("triaged", "triaged") {
		t.Fatal("idempotent status update rejected")
	}
}

func sqliteColumnExists(t *testing.T, conn *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := conn.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close schema rows: %v", err)
		}
	}()
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func testOwnerPrimaryTokenPersistence(t *testing.T, db *DB) {
	t.Helper()
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	ownerID, err := db.CreateOwnerPrimaryToken(context.Background(), hash, "own_abcdefgh", "WXYZ", "encrypted")
	if err != nil {
		t.Fatalf("create owner primary token: %v", err)
	}
	identity, valid, err := db.ResolveOwnerToken(context.Background(), "own_abcdefgh", hash)
	if err != nil || !valid || identity.OwnerID != ownerID || identity.Kind != "primary" {
		t.Fatalf("resolved owner token = %#v, valid=%v, err=%v", identity, valid, err)
	}
	var lastSeen, lastUsed int64
	if err := db.ServerDB.QueryRow(`SELECT owners.last_seen_at, api_tokens.last_used_at
		FROM owners JOIN api_tokens ON api_tokens.owner_id = owners.id WHERE api_tokens.token_hash = ?`, hash).Scan(&lastSeen, &lastUsed); err != nil {
		t.Fatalf("read owner activity timestamps: %v", err)
	}
	if lastSeen == 0 || lastUsed == 0 || lastSeen != lastUsed {
		t.Fatalf("owner/token activity timestamps = %d/%d, want matching nonzero timestamps", lastSeen, lastUsed)
	}
	if _, valid, err := db.ResolveOwnerToken(context.Background(), "bad", "not-a-digest"); err != nil || valid {
		t.Fatalf("malformed token digest resolved: valid=%v err=%v", valid, err)
	}
	if _, err := db.ServerDB.Exec("UPDATE api_tokens SET expires_at = ? WHERE token_hash = ?", time.Now().UTC().Unix()-1, hash); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := db.ResolveOwnerToken(context.Background(), "own_abcdefgh", hash); err != nil || valid {
		t.Fatalf("expired owner token resolved: valid=%v err=%v", valid, err)
	}
	if _, err := db.ServerDB.Exec("UPDATE api_tokens SET expires_at = NULL, revoked_at = ? WHERE token_hash = ?", time.Now().UTC().Unix(), hash); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := db.ResolveOwnerToken(context.Background(), "own_abcdefgh", hash); err != nil || valid {
		t.Fatalf("revoked owner token resolved: valid=%v err=%v", valid, err)
	}
	if _, err := db.CreateOwnerPrimaryToken(context.Background(), hash, "own_abcdefgh", "WXYZ", "encrypted"); err == nil {
		t.Fatal("duplicate token digest was accepted")
	}
	var count int
	if err := db.ServerDB.QueryRow("SELECT count(*) FROM owners WHERE id != 'system'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed token creation left an owner row: count=%d err=%v", count, err)
	}
}

func TestLegacyAPITokenIDsBecomeOpaqueUUIDv7(t *testing.T) {
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
	for _, statement := range []string{
		`CREATE TABLE owners (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL DEFAULT 1, last_seen_at INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE api_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
			kind TEXT NOT NULL, name TEXT, token_hash TEXT NOT NULL UNIQUE, token_prefix TEXT NOT NULL,
			token_last4 TEXT NOT NULL, token_enc TEXT NOT NULL, created_at INTEGER NOT NULL DEFAULT 1,
			rotated_at INTEGER, expires_at INTEGER, last_used_at INTEGER, revoked_at INTEGER, revoked_reason TEXT)`,
		`INSERT INTO owners (id) VALUES ('owner-legacy')`,
		`INSERT INTO api_tokens (id, owner_id, kind, token_hash, token_prefix, token_last4, token_enc)
			VALUES (7, 'owner-legacy', 'primary', '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', 'own_abcdefgh', 'WXYZ', 'encrypted')`,
	} {
		if _, err := db.ServerDB.Exec(statement); err != nil {
			t.Fatalf("prepare legacy token schema: %v", err)
		}
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("upgrade legacy token schema: %v", err)
	}
	var id string
	var legacyID int64
	if err := db.ServerDB.QueryRow("SELECT id, legacy_id FROM api_tokens WHERE token_prefix = ?", "own_abcdefgh").Scan(&id, &legacyID); err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || legacyID != 7 {
		t.Fatalf("migrated token id=%q legacy id=%d (parse err=%v)", id, legacyID, err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatalf("migrated schema is not idempotent: %v", err)
	}
}

func TestSchedulerStatePersistenceAndHistory(t *testing.T) {
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
	state := SchedulerTaskState{
		ID: "healthcheck_self", Name: "Self Health Check", Enabled: true,
		Schedule: "@every 5m", LastRun: 1_700_000_000, NextRun: 1_700_000_300,
		LastStatus: "success", RunCount: 8, FailCount: 1,
	}
	if err := db.SaveSchedulerTask(context.Background(), state); err != nil {
		t.Fatalf("save scheduler state: %v", err)
	}
	states, err := db.LoadSchedulerTasks(context.Background())
	if err != nil {
		t.Fatalf("load scheduler state: %v", err)
	}
	if got := states[state.ID]; got != state {
		t.Fatalf("scheduler state = %#v, want %#v", got, state)
	}
	started := time.Unix(state.LastRun, 0)
	if err := db.RecordSchedulerRun(context.Background(), state.ID, started, started.Add(2*time.Second), state.LastStatus, "", 2*time.Second); err != nil {
		t.Fatalf("record scheduler history: %v", err)
	}
	var historyCount int
	if err := db.ServerDB.QueryRow("SELECT count(*) FROM scheduler_history WHERE task_id = ?", state.ID).Scan(&historyCount); err != nil || historyCount != 1 {
		t.Fatalf("scheduler history rows = %d (err=%v), want 1", historyCount, err)
	}
}

func TestRateLimitCounterPersistence(t *testing.T) {
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
	want := []RateLimitRecord{{Key: "ip:192.0.2.1:strict:900", Count: 4, Previous: 3, WindowStart: 1_700_000_000}}
	if err := db.FlushRateLimits(context.Background(), want, time.Unix(1_700_000_040, 0)); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadRateLimits(context.Background())
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("persisted rate-limit counters = %#v, err=%v", got, err)
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
