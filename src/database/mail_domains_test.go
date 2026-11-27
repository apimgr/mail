package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/google/uuid"
)

func TestMailDomainReadModel(t *testing.T) {
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
	publicID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_domains
		(public_id, domain, description, owner_type, owner_id, max_mailboxes, max_aliases, max_quota_mb, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, publicID.String(), "example.test", "Example domain", "system", "system", 10, 20, 4096, 1); err != nil {
		t.Fatal(err)
	}

	listed, total, err := db.ListMailDomains(context.Background(), 1, 250)
	if err != nil || total != 1 {
		t.Fatalf("ListMailDomains() total=%d, err=%v", total, err)
	}
	if len(listed) != 1 || listed[0].ID != publicID.String() || listed[0].Domain != "example.test" || listed[0].Description != "Example domain" || listed[0].OwnerType != "system" || listed[0].OwnerID != "system" || listed[0].MaxMailboxes != 10 || listed[0].MaxAliases != 20 || listed[0].MaxQuotaMB != 4096 || !listed[0].Enabled {
		t.Fatalf("listed domains = %#v", listed)
	}

	got, err := db.GetMailDomain(context.Background(), "example.test")
	if err != nil || got.ID != listed[0].ID || got.Domain != listed[0].Domain || !got.Enabled {
		t.Fatalf("GetMailDomain() = %#v, %v", got, err)
	}
	if _, err := db.GetMailDomain(context.Background(), "missing.test"); !IsMailDomainNotFound(err) {
		t.Fatalf("missing domain error = %v", err)
	}
	if _, _, err := db.ListMailDomains(context.Background(), 1, 251); err == nil {
		t.Fatal("oversized domain page was accepted")
	}
}

func TestMailResourcesReferenceSystemOwner(t *testing.T) {
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

	for _, table := range []string{
		"srv_mail_servers", "mail_domains", "mail_mailboxes", "mail_aliases", "mail_forwards",
		"mail_dkim_keys", "mail_spf_records", "mail_dmarc_policies", "mail_pgp_keys",
		"mail_queue_log", "mail_delivery_log", "mail_stats_hourly", "mail_stats_daily",
		"mail_sieve_scripts", "mail_archive_policies", "mail_archived_messages",
	} {
		t.Run(table, func(t *testing.T) {
			rows, err := db.UsersReadDB.QueryContext(context.Background(), "PRAGMA foreign_key_list("+table+")")
			if err != nil {
				t.Fatal(err)
			}
			foundOwnerForeignKey := false
			for rows.Next() {
				var id, seq int
				var referencedTable, column, referencedColumn, onUpdate, onDelete, match string
				if err := rows.Scan(&id, &seq, &referencedTable, &column, &referencedColumn, &onUpdate, &onDelete, &match); err != nil {
					_ = rows.Close()
					t.Fatal(err)
				}
				if column == "owner_id" && referencedTable == "owners" && referencedColumn == "id" {
					foundOwnerForeignKey = true
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if !foundOwnerForeignKey {
				t.Errorf("%s.owner_id has no foreign key to owners(id)", table)
			}

			var ownerIndexCount int
			if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sqlite_master
				WHERE type = 'index' AND name = ?`, "idx_"+table+"_owner_id").Scan(&ownerIndexCount); err != nil {
				t.Fatal(err)
			}
			if ownerIndexCount != 1 {
				t.Errorf("%s.owner_id index count = %d, want 1", table, ownerIndexCount)
			}
		})
	}

	for _, statement := range []string{
		`INSERT INTO mail_domains (public_id, domain, owner_type, owner_id) VALUES ('bad-domain', 'bad.example', 'system', 'missing')`,
		`INSERT INTO mail_mailboxes (domain_id, owner_id, local_part, password) VALUES (1, 'missing', 'bad', 'hash')`,
		`INSERT INTO mail_aliases (domain_id, owner_id, source, destination) VALUES (1, 'missing', 'bad@example.test', 'target@example.test')`,
		`INSERT INTO mail_forwards (mailbox_id, owner_id, forward_to) VALUES (1, 'missing', 'target@example.test')`,
	} {
		if _, err := db.UsersDB.ExecContext(context.Background(), statement); err == nil {
			t.Errorf("orphaned mail owner was accepted by %q", statement)
		}
	}
}

func TestMailDomainLifecycleAndDNSConfiguration(t *testing.T) {
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
	domain, err := db.CreateMailDomain(ctx, MailDomainInput{
		Domain: "old.example", Description: "old description", MaxMailboxes: 5,
		MaxAliases: 7, MaxQuotaMB: 1024, Enabled: true,
	})
	if err != nil || domain.OwnerType != "system" || domain.OwnerID != "system" || domain.Domain != "old.example" {
		t.Fatalf("CreateMailDomain() = %#v, %v", domain, err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO srv_mail_servers
		(hostname, server_role, agent_token, primary_for_domains, relay_domains)
		VALUES ('mx.example', 'primary', 'token', 'old.example,other.example', 'old.example;other.example')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMailDomain(ctx, MailDomainInput{Domain: "second.example", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	firstPage, domainCount, err := db.ListMailDomains(ctx, 1, 1)
	if err != nil || domainCount != 2 || len(firstPage) != 1 {
		t.Fatalf("first domain page = %#v total=%d err=%v", firstPage, domainCount, err)
	}
	secondPage, domainCount, err := db.ListMailDomains(ctx, 2, 1)
	if err != nil || domainCount != 2 || len(secondPage) != 1 || firstPage[0].ID == secondPage[0].ID {
		t.Fatalf("second domain page = %#v total=%d err=%v", secondPage, domainCount, err)
	}
	if err := db.UpdateMailDomain(ctx, "old.example", MailDomainInput{
		Domain: "new.example", Description: "updated", MaxMailboxes: 9,
		MaxAliases: 11, MaxQuotaMB: 2048, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := db.GetMailDomain(ctx, "new.example")
	if err != nil || updated.Description != "updated" || updated.Enabled || updated.MaxQuotaMB != 2048 {
		t.Fatalf("updated domain = %#v, %v", updated, err)
	}
	var primaryDomains, relayDomains string
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT primary_for_domains, relay_domains FROM srv_mail_servers WHERE hostname = 'mx.example'`).Scan(&primaryDomains, &relayDomains); err != nil || primaryDomains != "new.example,other.example" || relayDomains != "new.example;other.example" {
		t.Fatalf("renamed server mappings = %q/%q, err=%v", primaryDomains, relayDomains, err)
	}
	if err := db.UpdateMailDomain(ctx, "missing.example", MailDomainInput{Domain: "other.example"}); err == nil {
		t.Fatal("updating a missing domain succeeded")
	}
	if _, err := db.UsersDB.ExecContext(ctx, `UPDATE srv_mail_servers SET mx_priority = 20 WHERE hostname = 'mx.example'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO mail_spf_records (domain_id, policy)
		VALUES ((SELECT id FROM mail_domains WHERE domain = 'new.example'), 'v=spf1 mx -all')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO mail_dmarc_policies (domain_id, policy, subdomain_policy, percentage, rua_email)
		VALUES ((SELECT id FROM mail_domains WHERE domain = 'new.example'), 'reject', 'quarantine', 75, 'dmarc@example.test')`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMailDKIMKey(ctx, "new.example", "mail", "enc:v1:ciphertext", "public-key"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMailDKIMKey(ctx, "new.example", "invalid", "plaintext", "public-key"); err == nil {
		t.Fatal("plaintext DKIM private key was accepted")
	}
	keys, err := db.ListMailDKIMKeys(ctx, "new.example")
	if err != nil || len(keys) != 1 || keys[0].Selector != "mail" || keys[0].PublicKey != "public-key" || keys[0].PrivateKey != "" {
		t.Fatalf("public DKIM keys = %#v, %v", keys, err)
	}
	records, missing, err := db.GetMailDNSRecords(ctx, "new.example")
	if err != nil || len(missing) != 0 || len(records) != 4 {
		t.Fatalf("DNS records = %#v missing=%v err=%v", records, missing, err)
	}
	if records[0].Type != "MX" || records[0].Value != "mx.example" || records[0].Preference == nil || *records[0].Preference != 20 {
		t.Fatalf("MX DNS record = %#v", records[0])
	}
	if err := db.DeleteMailDomain(ctx, "new.example"); !errors.Is(err, ErrMailDomainInUse) {
		t.Fatalf("delete with dependencies error = %v", err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `DELETE FROM mail_spf_records WHERE domain_id = (SELECT id FROM mail_domains WHERE domain = 'new.example')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `DELETE FROM mail_dmarc_policies WHERE domain_id = (SELECT id FROM mail_domains WHERE domain = 'new.example')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `DELETE FROM mail_dkim_keys WHERE domain_id = (SELECT id FROM mail_domains WHERE domain = 'new.example')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `UPDATE srv_mail_servers SET primary_for_domains = '', relay_domains = ''`); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMailDomain(ctx, "new.example"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMailDomain(ctx, "new.example"); err == nil {
		t.Fatal("deleting a missing domain succeeded")
	}
}

func TestMailAliasPersistenceUsesOpaqueIDsAndPages(t *testing.T) {
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
	if _, err := db.CreateMailDomain(ctx, MailDomainInput{Domain: "example.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO mail_aliases (domain_id, source, destination)
		VALUES ((SELECT id FROM mail_domains WHERE domain = 'example.test'), 'legacy@example.test', 'ops@example.test')`); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	var legacyID string
	var legacyOwnerType, legacyOwnerID string
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT public_id, owner_type, owner_id FROM mail_aliases WHERE source = 'legacy@example.test'`).Scan(&legacyID, &legacyOwnerType, &legacyOwnerID); err != nil {
		t.Fatal(err)
	}
	if legacyOwnerType != "system" || legacyOwnerID != "system" {
		t.Fatalf("legacy alias owner = %q/%q", legacyOwnerType, legacyOwnerID)
	}
	legacyUUID, err := uuid.Parse(legacyID)
	if err != nil || legacyUUID.Version() != 7 {
		t.Fatalf("backfilled alias ID = %q, err=%v", legacyID, err)
	}
	created, err := db.CreateMailAlias(ctx, "example.test", "info@example.test", "ops@example.test")
	if err != nil {
		t.Fatal(err)
	}
	var ownerType, ownerID string
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT owner_type, owner_id FROM mail_aliases WHERE public_id = ?`, created.ID).Scan(&ownerType, &ownerID); err != nil || ownerType != "system" || ownerID != "system" {
		t.Fatalf("created alias owner = %q/%q, err=%v", ownerType, ownerID, err)
	}
	parsed, err := uuid.Parse(created.ID)
	if err != nil || parsed.Version() != 7 || created.Source != "info@example.test" || created.Destination != "ops@example.test" || created.Domain != "example.test" || !created.Enabled {
		t.Fatalf("created alias = %#v, parsed ID err=%v", created, err)
	}
	aliases, total, err := db.ListMailAliases(ctx, 1, 1)
	if err != nil || total != 2 || len(aliases) != 1 {
		t.Fatalf("alias page = %#v total=%d err=%v", aliases, total, err)
	}
	secondPage, total, err := db.ListMailAliases(ctx, 2, 1)
	if err != nil || total != 2 || len(secondPage) != 1 || (aliases[0].ID != created.ID && secondPage[0].ID != created.ID) {
		t.Fatalf("second alias page = %#v total=%d err=%v", secondPage, total, err)
	}
	if _, _, err := db.ListMailAliases(ctx, 0, 1); err == nil {
		t.Fatal("invalid alias page was accepted")
	}
	if err := db.DeleteMailAlias(ctx, "missing"); !IsMailDomainNotFound(err) {
		t.Fatalf("deleting missing alias error = %v", err)
	}
	if err := db.DeleteMailAlias(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	aliases, total, err = db.ListMailAliases(ctx, 1, 250)
	if err != nil || total != 1 || len(aliases) != 1 {
		t.Fatalf("aliases after delete = %#v total=%d err=%v", aliases, total, err)
	}
}

func TestMailUserReadModelDoesNotExposePasswordHash(t *testing.T) {
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
	domain, err := db.CreateMailDomain(ctx, MailDomainInput{Domain: "example.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	publicID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO mail_mailboxes
		(public_id, domain_id, local_part, password, quota_bytes, enabled, created_at, updated_at)
		VALUES (?, (SELECT id FROM mail_domains WHERE domain = ?), ?, ?, ?, ?, ?, ?)`,
		publicID.String(), domain.Domain, "info", "must-not-be-returned", int64(4096), 1, int64(10), int64(20)); err != nil {
		t.Fatal(err)
	}
	users, total, err := db.ListMailUsers(ctx, 1, 250)
	if err != nil || total != 1 || len(users) != 1 {
		t.Fatalf("ListMailUsers() = %#v total=%d err=%v", users, total, err)
	}
	want := MailUser{ID: publicID.String(), Email: "info@example.test", LocalPart: "info", Domain: "example.test", QuotaBytes: 4096, Enabled: true, CreatedAt: 10, UpdatedAt: 20}
	if users[0] != want {
		t.Fatalf("listed user = %#v, want %#v", users[0], want)
	}
	got, err := db.GetMailUser(ctx, publicID.String())
	if err != nil || got != want {
		t.Fatalf("GetMailUser() = %#v, %v; want %#v", got, err, want)
	}
	if _, err := db.GetMailUser(ctx, "missing"); !IsMailDomainNotFound(err) {
		t.Fatalf("missing mail user error = %v", err)
	}
	if _, _, err := db.ListMailUsers(ctx, 0, 1); err == nil {
		t.Fatal("invalid mail user page was accepted")
	}
}

func TestMailUserWriteLifecycle(t *testing.T) {
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
	if _, err := db.CreateMailDomain(ctx, MailDomainInput{Domain: "example.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateMailUser(ctx, "info", "example.test", "$argon2id$test", 2048, true)
	if err != nil || user.Email != "info@example.test" || user.QuotaBytes != 2048 || !user.Enabled {
		t.Fatalf("CreateMailUser() = %#v, %v", user, err)
	}
	if _, err := db.CreateMailUser(ctx, "info", "example.test", "$argon2id$other", 0, true); !errors.Is(err, ErrMailUserConflict) {
		t.Fatalf("duplicate CreateMailUser() error = %v", err)
	}
	quota := int64(0)
	enabled := false
	if err := db.UpdateMailUserFields(ctx, user.ID, &quota, &enabled); err != nil {
		t.Fatal(err)
	}
	user, err = db.GetMailUser(ctx, user.ID)
	if err != nil || user.QuotaBytes != 0 || user.Enabled {
		t.Fatalf("updated mail user = %#v, %v", user, err)
	}
	if err := db.ChangeMailUserPassword(ctx, user.ID, "$argon2id$new"); err != nil {
		t.Fatal(err)
	}
	var storedPassword string
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT password FROM mail_mailboxes WHERE public_id = ?`, user.ID).Scan(&storedPassword); err != nil || storedPassword != "$argon2id$new" {
		t.Fatalf("stored password verifier = %q, err=%v", storedPassword, err)
	}
	if err := db.DeleteMailUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	dependent, err := db.CreateMailUser(ctx, "forwarded", "example.test", "$argon2id$test", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	var internalID int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT id FROM mail_mailboxes WHERE public_id = ?`, dependent.ID).Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO mail_forwards (mailbox_id, forward_to) VALUES (?, ?)`, internalID, "other@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMailUser(ctx, dependent.ID); !errors.Is(err, ErrMailUserHasDependencies) {
		t.Fatalf("mail user with forward delete error = %v", err)
	}
}

func TestMailDomainSchemaBackfillsOpaqueSystemOwnedIDs(t *testing.T) {
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
	if _, err := db.ServerDB.ExecContext(context.Background(), `CREATE TABLE mail_domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE, description TEXT,
		owner_type TEXT NOT NULL, owner_id INTEGER NOT NULL, max_mailboxes INTEGER DEFAULT 0,
		max_aliases INTEGER DEFAULT 0, max_quota_mb INTEGER DEFAULT 0, enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_domains
		(domain, owner_type, owner_id) VALUES ('legacy.example', 'operator', 7)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_domains
		(domain, owner_type, owner_id) VALUES ('legacy-owner.example', 'owner', 9)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_domains
		(domain, owner_type, owner_id) VALUES ('legacy-system.example', 'system', 11)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `CREATE TABLE mail_aliases (
		id INTEGER PRIMARY KEY AUTOINCREMENT, domain_id INTEGER NOT NULL,
		source TEXT NOT NULL, destination TEXT NOT NULL,
		enabled INTEGER DEFAULT 1, created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_aliases (domain_id, source, destination)
		VALUES (1, 'legacy@example.test', 'ops@example.test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `CREATE TABLE mail_mailboxes (
		id INTEGER PRIMARY KEY AUTOINCREMENT, domain_id INTEGER NOT NULL, local_part TEXT NOT NULL,
		password TEXT NOT NULL, name TEXT, quota_mb INTEGER DEFAULT 0, enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_mailboxes (domain_id, local_part, password, quota_mb)
		VALUES (1, 'legacy', 'hash', 2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `CREATE TABLE mail_forwards (
		id INTEGER PRIMARY KEY AUTOINCREMENT, mailbox_id INTEGER NOT NULL, forward_to TEXT NOT NULL,
		keep_local INTEGER DEFAULT 1, enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_forwards (mailbox_id, forward_to)
		VALUES (1, 'forward@example.test')`); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ServerReadDB.QueryContext(context.Background(), `SELECT public_id, owner_type, owner_id FROM mail_domains ORDER BY domain`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var publicID, ownerType, ownerID string
		if err := rows.Scan(&publicID, &ownerType, &ownerID); err != nil {
			t.Fatal(err)
		}
		parsed, err := uuid.Parse(publicID)
		if err != nil || parsed.Version() != 7 {
			t.Errorf("migrated public ID = %q, err=%v; want UUIDv7", publicID, err)
		}
		if ownerType != "system" || ownerID != "system" {
			t.Errorf("migrated owner = %q/%q, want system/system", ownerType, ownerID)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("migrated domain count = %d, want 3", count)
	}
	var aliasID, aliasOwnerType, aliasOwnerID string
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT public_id, owner_type, owner_id FROM mail_aliases WHERE source = 'legacy@example.test'`).Scan(&aliasID, &aliasOwnerType, &aliasOwnerID); err != nil {
		t.Fatal(err)
	}
	parsedAliasID, err := uuid.Parse(aliasID)
	if err != nil || parsedAliasID.Version() != 7 || aliasOwnerType != "system" || aliasOwnerID != "system" {
		t.Fatalf("migrated alias = id %q owner %q/%q err=%v", aliasID, aliasOwnerType, aliasOwnerID, err)
	}
	for _, table := range []string{"mail_mailboxes", "mail_forwards"} {
		var ownerType, ownerID string
		if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT owner_type, owner_id FROM `+table+` LIMIT 1`).Scan(&ownerType, &ownerID); err != nil {
			t.Fatal(err)
		}
		if ownerType != "system" || ownerID != "system" {
			t.Errorf("migrated %s owner = %q/%q", table, ownerType, ownerID)
		}
	}
	var forwardPublicID string
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT public_id FROM mail_forwards WHERE mailbox_id = 1`).Scan(&forwardPublicID); err != nil {
		t.Fatal(err)
	}
	parsedForwardID, err := uuid.Parse(forwardPublicID)
	if err != nil || parsedForwardID.Version() != 7 {
		t.Fatalf("migrated forward public ID = %q, err=%v; want UUIDv7", forwardPublicID, err)
	}
	var mailboxPublicID string
	var quotaBytes int64
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT public_id, quota_bytes FROM mail_mailboxes WHERE local_part = 'legacy'`).Scan(&mailboxPublicID, &quotaBytes); err != nil {
		t.Fatal(err)
	}
	parsedMailboxID, err := uuid.Parse(mailboxPublicID)
	if err != nil || parsedMailboxID.Version() != 7 || quotaBytes != 2*1024*1024 {
		t.Fatalf("migrated mailbox ID/quota = %q/%d, err=%v", mailboxPublicID, quotaBytes, err)
	}
}
