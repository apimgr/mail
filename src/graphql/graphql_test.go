package graphql

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/mailservice"
)

func TestSchemaParsesWithEveryReadResolver(t *testing.T) {
	if _, err := NewSchema(nil, nil); err != nil {
		t.Fatalf("parse mail GraphQL schema: %v", err)
	}
}

func TestReadResolversExecuteAndRejectInvalidPagination(t *testing.T) {
	schema, err := NewSchema(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := schema.Exec(context.Background(), `{ mailServices { name status executable packages serviceAccount } }`, "", nil)
	if len(result.Errors) != 0 || !strings.Contains(string(result.Data), `"mailServices"`) {
		t.Fatalf("mailServices query = %s, errors = %v", result.Data, result.Errors)
	}
	result = schema.Exec(context.Background(), `{ mailUsers(page: 0) { pagination { page } } }`, "", nil)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "pagination") {
		t.Fatalf("invalid pagination errors = %v", result.Errors)
	}
}

type testQueue struct {
	entries []mailservice.QueueEntry
}

func (queue testQueue) List(context.Context) ([]mailservice.QueueEntry, error) {
	return queue.entries, nil
}

func (queue testQueue) Get(_ context.Context, id string) (mailservice.QueueEntry, error) {
	for _, entry := range queue.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return mailservice.QueueEntry{}, mailservice.ErrQueueNotFound
}

func (testQueue) Operate(context.Context, string, string) error { return nil }

func TestReadResolversReturnStoredMailAndQueueData(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
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
	domain, err := db.CreateMailDomain(ctx, database.MailDomainInput{Domain: "example.test", Description: "Example", MaxMailboxes: 5, MaxAliases: 8, MaxQuotaMB: 1024, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateMailUser(ctx, "info", domain.Domain, "$argon2id$test", 2048, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMailAlias(ctx, domain.Domain, "help@example.test", "info@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMailForward(ctx, user.ID, []string{"admin@example.net"}, true, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMailDKIMKey(ctx, domain.Domain, "mail", "enc:v1:test", "public-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(ctx, `INSERT INTO srv_mail_servers (hostname, server_role, agent_token, primary_for_domains, mx_priority) VALUES (?, 'primary', 'test', ?, 10)`, "mx.example.test", domain.Domain); err != nil {
		t.Fatal(err)
	}
	queueEntry := mailservice.QueueEntry{ID: "AB123", Queue: "active", Size: 4096, Sender: "sender@example.net", Recipients: []string{"info@example.test"}, ArrivalTime: time.Unix(1700000000, 0).UTC(), Reason: "deferred"}
	resolver := &queryResolver{db: db, queue: testQueue{entries: []mailservice.QueueEntry{queueEntry}}}

	gotUser, err := resolver.MailUser(ctx, userArgs{ID: user.ID})
	if err != nil || gotUser.Email != "info@example.test" || gotUser.Kind != "virtual" || gotUser.QuotaBytes != 2048 {
		t.Fatalf("MailUser() = %#v, %v", gotUser, err)
	}
	users, err := resolver.MailUsers(ctx, paginationArgs{})
	if err != nil || len(users.Data) != 1 || users.Pagination.Total != 1 {
		t.Fatalf("MailUsers() = %#v, %v", users, err)
	}
	gotDomain, err := resolver.MailDomain(ctx, domainArgs{Domain: " EXAMPLE.TEST. "})
	if err != nil || gotDomain.ID != domain.ID || gotDomain.Description != "Example" {
		t.Fatalf("MailDomain() = %#v, %v", gotDomain, err)
	}
	domains, err := resolver.MailDomains(ctx, paginationArgs{})
	if err != nil || len(domains.Data) != 1 || domains.Data[0].MaxQuotaMB != 1024 {
		t.Fatalf("MailDomains() = %#v, %v", domains, err)
	}
	dns, err := resolver.MailDNS(ctx, domainArgs{Domain: domain.Domain})
	if err != nil || len(dns.Records) == 0 || dns.Records[0].Type != "MX" {
		t.Fatalf("MailDNS() = %#v, %v", dns, err)
	}
	dkim, err := resolver.MailDKIM(ctx, domainArgs{Domain: domain.Domain})
	if err != nil || len(dkim) != 1 || !strings.Contains(dkim[0].Record, "public-key") {
		t.Fatalf("MailDKIM() = %#v, %v", dkim, err)
	}
	aliases, err := resolver.MailAliases(ctx, paginationArgs{})
	if err != nil || len(aliases.Data) != 1 || aliases.Data[0].Source != "help@example.test" {
		t.Fatalf("MailAliases() = %#v, %v", aliases, err)
	}
	forwards, err := resolver.MailForwards(ctx, paginationArgs{})
	if err != nil || len(forwards.Data) != 1 || !forwards.Data[0].KeepLocal {
		t.Fatalf("MailForwards() = %#v, %v", forwards, err)
	}
	queuePage, err := resolver.MailQueue(ctx, paginationArgs{})
	if err != nil || len(queuePage.Data) != 1 || queuePage.Data[0].ArrivalTime == "" {
		t.Fatalf("MailQueue() = %#v, %v", queuePage, err)
	}
	gotEntry, err := resolver.MailQueueEntry(ctx, entryArgs{ID: queueEntry.ID})
	if err != nil || gotEntry.Reason != "deferred" {
		t.Fatalf("MailQueueEntry() = %#v, %v", gotEntry, err)
	}
	stats, err := resolver.MailQueueStats(ctx)
	if err != nil || stats.CurrentQueueSize != 1 || stats.TotalSizeBytes != 4096 || len(stats.ByState) != 1 {
		t.Fatalf("MailQueueStats() = %#v, %v", stats, err)
	}
	if _, err := normalizeGraphDomain("not/a/domain"); err == nil {
		t.Fatal("invalid domain was accepted")
	}
	if _, err := resolver.MailUser(ctx, userArgs{ID: "missing"}); err == nil {
		t.Fatal("missing mail user was accepted")
	}
}
