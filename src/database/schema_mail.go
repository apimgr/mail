package database

// schema_mail.go contains mail-specific database tables for users.db
// These tables support the mail infrastructure management features

// Mail infrastructure tables for users.db
var mailCreateStatements = []string{
	// Mail servers - per PLAN.AI.md line 1121
	`CREATE TABLE IF NOT EXISTS srv_mail_servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		hostname TEXT NOT NULL UNIQUE,
		ip_address TEXT,
		server_role TEXT NOT NULL,
		agent_token TEXT NOT NULL,
		agent_port INTEGER DEFAULT 64100,
		mx_priority INTEGER NOT NULL DEFAULT 10,
		primary_for_domains TEXT,
		relay_domains TEXT,
		relay_to_host TEXT,
		smarthost TEXT,
		postfix_running INTEGER DEFAULT 0,
		dovecot_running INTEGER DEFAULT 0,
		amavisd_running INTEGER DEFAULT 0,
		clamav_running INTEGER DEFAULT 0,
		spam_filter_running INTEGER DEFAULT 0,
		last_seen INTEGER,
		status TEXT DEFAULT 'unknown',
		load_average TEXT,
		disk_usage_percent INTEGER,
		queue_size INTEGER,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		enabled INTEGER DEFAULT 1
	)`,

	// Mail domains - virtual domains hosted
	`CREATE TABLE IF NOT EXISTS mail_domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		public_id TEXT NOT NULL UNIQUE,
		domain TEXT NOT NULL UNIQUE,
		description TEXT,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL REFERENCES owners(id),
		max_mailboxes INTEGER DEFAULT 0,
		max_aliases INTEGER DEFAULT 0,
		max_quota_mb INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_domains_owner ON mail_domains(owner_type, owner_id)`,

	// Mail mailboxes - virtual mailboxes
	`CREATE TABLE IF NOT EXISTS mail_mailboxes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		public_id TEXT UNIQUE,
		domain_id INTEGER NOT NULL,
		owner_type TEXT NOT NULL DEFAULT 'system',
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		local_part TEXT NOT NULL,
		password TEXT NOT NULL,
		name TEXT,
		quota_mb INTEGER DEFAULT 0,
		quota_bytes INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		UNIQUE(domain_id, local_part)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_mailboxes_domain ON mail_mailboxes(domain_id)`,

	// Mail aliases - email aliases
	`CREATE TABLE IF NOT EXISTS mail_aliases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		public_id TEXT UNIQUE,
		domain_id INTEGER NOT NULL,
		owner_type TEXT NOT NULL DEFAULT 'system',
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_aliases_domain ON mail_aliases(domain_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_aliases_source ON mail_aliases(source)`,

	// Mail forwards - forwarding rules
	`CREATE TABLE IF NOT EXISTS mail_forwards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		mailbox_id INTEGER NOT NULL,
		owner_type TEXT NOT NULL DEFAULT 'system',
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		forward_to TEXT NOT NULL,
		keep_local INTEGER DEFAULT 1,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_forwards_mailbox ON mail_forwards(mailbox_id)`,

	// DKIM keys - per-domain DKIM signing keys
	`CREATE TABLE IF NOT EXISTS mail_dkim_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		domain_id INTEGER NOT NULL,
		selector TEXT NOT NULL,
		private_key TEXT NOT NULL,
		public_key TEXT NOT NULL,
		key_size INTEGER DEFAULT 2048,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		expires_at INTEGER,
		UNIQUE(domain_id, selector)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_dkim_domain ON mail_dkim_keys(domain_id)`,

	// SPF records
	`CREATE TABLE IF NOT EXISTS mail_spf_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		domain_id INTEGER NOT NULL UNIQUE,
		policy TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	// DMARC policies
	`CREATE TABLE IF NOT EXISTS mail_dmarc_policies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		domain_id INTEGER NOT NULL UNIQUE,
		policy TEXT NOT NULL DEFAULT 'none',
		subdomain_policy TEXT,
		percentage INTEGER DEFAULT 100,
		rua_email TEXT,
		ruf_email TEXT,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	// PGP/GPG keys
	`CREATE TABLE IF NOT EXISTS mail_pgp_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		mailbox_id INTEGER NOT NULL,
		fingerprint TEXT NOT NULL UNIQUE,
		public_key TEXT NOT NULL,
		private_key TEXT,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		expires_at INTEGER
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_pgp_mailbox ON mail_pgp_keys(mailbox_id)`,

	// Mail queue log
	`CREATE TABLE IF NOT EXISTS mail_queue_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		queue_id TEXT NOT NULL,
		mail_server_id INTEGER,
		sender TEXT NOT NULL,
		recipient TEXT NOT NULL,
		size_bytes INTEGER,
		status TEXT NOT NULL,
		timestamp INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_queue_timestamp ON mail_queue_log(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_queue_server ON mail_queue_log(mail_server_id)`,

	// Mail delivery log
	`CREATE TABLE IF NOT EXISTS mail_delivery_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		queue_id TEXT,
		mail_server_id INTEGER,
		sender TEXT NOT NULL,
		recipient TEXT NOT NULL,
		size_bytes INTEGER,
		status TEXT NOT NULL,
		delay_seconds INTEGER,
		dsn TEXT,
		timestamp INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_delivery_timestamp ON mail_delivery_log(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_delivery_recipient ON mail_delivery_log(recipient)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_delivery_server ON mail_delivery_log(mail_server_id)`,

	// Mail stats hourly
	`CREATE TABLE IF NOT EXISTS mail_stats_hourly (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		mail_server_id INTEGER NOT NULL,
		hour_timestamp INTEGER NOT NULL,
		sent_count INTEGER DEFAULT 0,
		received_count INTEGER DEFAULT 0,
		bounced_count INTEGER DEFAULT 0,
		deferred_count INTEGER DEFAULT 0,
		spam_count INTEGER DEFAULT 0,
		virus_count INTEGER DEFAULT 0,
		total_size_bytes INTEGER DEFAULT 0,
		UNIQUE(mail_server_id, hour_timestamp)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_stats_hourly_server ON mail_stats_hourly(mail_server_id)`,
	`CREATE INDEX IF NOT EXISTS idx_stats_hourly_time ON mail_stats_hourly(hour_timestamp)`,

	// Mail stats daily
	`CREATE TABLE IF NOT EXISTS mail_stats_daily (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		mail_server_id INTEGER NOT NULL,
		day_timestamp INTEGER NOT NULL,
		sent_count INTEGER DEFAULT 0,
		received_count INTEGER DEFAULT 0,
		bounced_count INTEGER DEFAULT 0,
		deferred_count INTEGER DEFAULT 0,
		spam_count INTEGER DEFAULT 0,
		virus_count INTEGER DEFAULT 0,
		total_size_bytes INTEGER DEFAULT 0,
		UNIQUE(mail_server_id, day_timestamp)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_stats_daily_server ON mail_stats_daily(mail_server_id)`,
	`CREATE INDEX IF NOT EXISTS idx_stats_daily_time ON mail_stats_daily(day_timestamp)`,

	// Sieve scripts - mail filtering
	`CREATE TABLE IF NOT EXISTS mail_sieve_scripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		mailbox_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		script_content TEXT NOT NULL,
		priority INTEGER DEFAULT 0,
		active INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_sieve_mailbox ON mail_sieve_scripts(mailbox_id)`,

	// Archive policies
	`CREATE TABLE IF NOT EXISTS mail_archive_policies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		domain_id INTEGER,
		mailbox_id INTEGER,
		archive_after_days INTEGER DEFAULT 365,
		delete_after_days INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_archive_domain ON mail_archive_policies(domain_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_archive_mailbox ON mail_archive_policies(mailbox_id)`,

	// Archived messages
	`CREATE TABLE IF NOT EXISTS mail_archived_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL DEFAULT 'system' REFERENCES owners(id),
		mailbox_id INTEGER NOT NULL,
		message_id TEXT NOT NULL,
		subject TEXT,
		sender TEXT,
		recipient TEXT,
		size_bytes INTEGER,
		archived_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		delete_at INTEGER,
		storage_path TEXT
	)`,

	`CREATE INDEX IF NOT EXISTS idx_mail_archived_mailbox ON mail_archived_messages(mailbox_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_archived_delete ON mail_archived_messages(delete_at)`,
}

// CalDAV tables for users.db
var caldavCreateStatements = []string{
	// Calendars
	`CREATE TABLE IF NOT EXISTS cal_calendars (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_type TEXT NOT NULL,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		color TEXT DEFAULT '#3b82f6',
		timezone TEXT DEFAULT 'UTC',
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_cal_calendars_owner ON cal_calendars(owner_type, owner_id)`,

	// Calendar events
	`CREATE TABLE IF NOT EXISTS cal_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		calendar_id INTEGER NOT NULL,
		uid TEXT NOT NULL UNIQUE,
		summary TEXT NOT NULL,
		description TEXT,
		location TEXT,
		start_time INTEGER NOT NULL,
		end_time INTEGER NOT NULL,
		all_day INTEGER DEFAULT 0,
		recurring INTEGER DEFAULT 0,
		recurrence_rule TEXT,
		ical_data TEXT NOT NULL,
		etag TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_cal_events_calendar ON cal_events(calendar_id)`,
	`CREATE INDEX IF NOT EXISTS idx_cal_events_start ON cal_events(start_time)`,

	// Event attendees
	`CREATE TABLE IF NOT EXISTS cal_attendees (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		email TEXT NOT NULL,
		name TEXT,
		role TEXT DEFAULT 'REQ-PARTICIPANT',
		status TEXT DEFAULT 'NEEDS-ACTION',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_cal_attendees_event ON cal_attendees(event_id)`,

	// Calendar shares
	`CREATE TABLE IF NOT EXISTS cal_shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		calendar_id INTEGER NOT NULL,
		shared_with_type TEXT NOT NULL,
		shared_with_id INTEGER NOT NULL,
		permission TEXT NOT NULL DEFAULT 'read',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		UNIQUE(calendar_id, shared_with_type, shared_with_id)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_cal_shares_calendar ON cal_shares(calendar_id)`,
}

// CardDAV tables for users.db
var carddavCreateStatements = []string{
	// Address books
	`CREATE TABLE IF NOT EXISTS card_addressbooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_type TEXT NOT NULL,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_card_addressbooks_owner ON card_addressbooks(owner_type, owner_id)`,

	// Contacts
	`CREATE TABLE IF NOT EXISTS card_contacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		addressbook_id INTEGER NOT NULL,
		uid TEXT NOT NULL UNIQUE,
		full_name TEXT NOT NULL,
		given_name TEXT,
		family_name TEXT,
		email TEXT,
		phone TEXT,
		organization TEXT,
		vcard_data TEXT NOT NULL,
		etag TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_card_contacts_addressbook ON card_contacts(addressbook_id)`,
	`CREATE INDEX IF NOT EXISTS idx_card_contacts_name ON card_contacts(full_name)`,
	`CREATE INDEX IF NOT EXISTS idx_card_contacts_email ON card_contacts(email)`,

	// Address book shares
	`CREATE TABLE IF NOT EXISTS card_shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		addressbook_id INTEGER NOT NULL,
		shared_with_type TEXT NOT NULL,
		shared_with_id INTEGER NOT NULL,
		permission TEXT NOT NULL DEFAULT 'read',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		UNIQUE(addressbook_id, shared_with_type, shared_with_id)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_card_shares_addressbook ON card_shares(addressbook_id)`,
}

// Mailing list tables for users.db
var mailingListCreateStatements = []string{
	// Mailing lists
	`CREATE TABLE IF NOT EXISTS list_lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_type TEXT NOT NULL,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL UNIQUE,
		email TEXT NOT NULL UNIQUE,
		description TEXT,
		list_type TEXT NOT NULL DEFAULT 'discussion',
		moderated INTEGER DEFAULT 0,
		public_archive INTEGER DEFAULT 1,
		subscribe_policy TEXT DEFAULT 'open',
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_list_lists_owner ON list_lists(owner_type, owner_id)`,

	// List members
	`CREATE TABLE IF NOT EXISTS list_members (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL,
		email TEXT NOT NULL,
		name TEXT,
		delivery_mode TEXT DEFAULT 'enabled',
		moderator INTEGER DEFAULT 0,
		subscribed_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		UNIQUE(list_id, email)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_list_members_list ON list_members(list_id)`,

	// List archives
	`CREATE TABLE IF NOT EXISTS list_archives (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL,
		message_id TEXT NOT NULL,
		sender TEXT NOT NULL,
		subject TEXT NOT NULL,
		body TEXT NOT NULL,
		posted_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_list_archives_list ON list_archives(list_id)`,
	`CREATE INDEX IF NOT EXISTS idx_list_archives_posted ON list_archives(posted_at)`,
}

// mailSchemaUpdates contains idempotent schema updates for mail tables
var mailSchemaUpdates = []string{
	// Version devel (unreleased, 2026-11-26): add the configured MX preference for mail servers.
	`ALTER TABLE srv_mail_servers ADD COLUMN mx_priority INTEGER NOT NULL DEFAULT 10`,
	// Version devel (unreleased, 2026-11-26): add opaque UUIDv7 IDs for mail domains.
	`ALTER TABLE mail_domains ADD COLUMN public_id TEXT`,
	// Version devel (unreleased, 2026-11-26): uniquely index opaque mail-domain IDs.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_domains_public_id ON mail_domains(public_id)`,
	// Version devel (unreleased, 2026-11-26): map legacy mail-domain owners to the system owner.
	`UPDATE mail_domains SET owner_type = 'system', owner_id = 'system' WHERE owner_type <> 'system' OR owner_id <> 'system'`,
	`CREATE INDEX IF NOT EXISTS idx_mail_domains_owner_id ON mail_domains(owner_id)`,
	// Version devel (unreleased, 2026-11-26): add opaque UUIDv7 identifiers for mail aliases.
	`ALTER TABLE mail_aliases ADD COLUMN public_id TEXT`,
	// Version devel (unreleased, 2026-11-26): uniquely index opaque mail-alias IDs.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_aliases_public_id ON mail_aliases(public_id)`,
	// Version devel (unreleased, 2026-11-26): add canonical ownership to mail aliases.
	`ALTER TABLE mail_aliases ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'system'`,
	// Version devel (unreleased, 2026-11-27): add nullable foreign-key ownership to legacy mail aliases.
	`ALTER TABLE mail_aliases ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	// Version devel (unreleased, 2026-11-27): backfill legacy mail aliases to the reserved system owner.
	`UPDATE mail_aliases SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): normalize legacy mail aliases to the system owner.
	`UPDATE mail_aliases SET owner_type = 'system', owner_id = 'system' WHERE owner_type <> 'system' OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): index mail alias ownership lookups.
	`CREATE INDEX IF NOT EXISTS idx_mail_aliases_owner ON mail_aliases(owner_type, owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_aliases_owner_id ON mail_aliases(owner_id)`,
	// Version devel (unreleased, 2026-11-26): add canonical ownership to mail mailboxes.
	`ALTER TABLE mail_mailboxes ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'system'`,
	// Version devel (unreleased, 2026-11-27): add nullable foreign-key ownership to legacy mailboxes.
	`ALTER TABLE mail_mailboxes ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	// Version devel (unreleased, 2026-11-27): backfill legacy mailboxes to the reserved system owner.
	`UPDATE mail_mailboxes SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): normalize legacy mail mailboxes to the system owner.
	`UPDATE mail_mailboxes SET owner_type = 'system', owner_id = 'system' WHERE owner_type <> 'system' OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): index mail mailbox ownership lookups.
	`CREATE INDEX IF NOT EXISTS idx_mail_mailboxes_owner ON mail_mailboxes(owner_type, owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_mailboxes_owner_id ON mail_mailboxes(owner_id)`,
	// Version devel (unreleased, 2026-11-26): add canonical ownership to mail forwards.
	`ALTER TABLE mail_forwards ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'system'`,
	// Version devel (unreleased, 2026-11-27): add nullable foreign-key ownership to legacy forwards.
	`ALTER TABLE mail_forwards ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	// Version devel (unreleased, 2026-11-27): backfill legacy forwards to the reserved system owner.
	`UPDATE mail_forwards SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): normalize legacy mail forwards to the system owner.
	`UPDATE mail_forwards SET owner_type = 'system', owner_id = 'system' WHERE owner_type <> 'system' OR owner_id <> 'system'`,
	// Version devel (unreleased, 2026-11-26): index mail forward ownership lookups.
	`CREATE INDEX IF NOT EXISTS idx_mail_forwards_owner ON mail_forwards(owner_type, owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_forwards_owner_id ON mail_forwards(owner_id)`,
	// Version devel (unreleased, 2026-11-26): add opaque UUIDv7 identifiers to mail forwards.
	`ALTER TABLE mail_forwards ADD COLUMN public_id TEXT`,
	// Version devel (unreleased, 2026-11-26): uniquely index opaque mail-forward IDs.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_forwards_public_id ON mail_forwards(public_id)`,
	// Version devel (unreleased, 2026-11-26): add opaque UUIDv7 IDs to virtual mailboxes.
	`ALTER TABLE mail_mailboxes ADD COLUMN public_id TEXT`,
	// Version devel (unreleased, 2026-11-26): add mailbox quotas in bytes without removing the legacy field.
	`ALTER TABLE mail_mailboxes ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0`,
	// Version devel (unreleased, 2026-11-26): uniquely index opaque mailbox IDs.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_mailboxes_public_id ON mail_mailboxes(public_id)`,
	// Version devel (unreleased, 2026-11-26): preserve existing mailbox quotas when adding byte-based quotas.
	`UPDATE mail_mailboxes SET quota_bytes = quota_mb * 1048576 WHERE quota_bytes = 0 AND quota_mb > 0`,
	// Version devel (unreleased, 2026-11-27): backfill global mail records to the reserved system owner.
	`ALTER TABLE srv_mail_servers ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE srv_mail_servers SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_dkim_keys ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_dkim_keys SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_spf_records ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_spf_records SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_dmarc_policies ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_dmarc_policies SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_pgp_keys ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_pgp_keys SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_queue_log ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_queue_log SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_delivery_log ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_delivery_log SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_stats_hourly ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_stats_hourly SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_stats_daily ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_stats_daily SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_sieve_scripts ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_sieve_scripts SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_archive_policies ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_archive_policies SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`ALTER TABLE mail_archived_messages ADD COLUMN owner_id TEXT REFERENCES owners(id)`,
	`UPDATE mail_archived_messages SET owner_id = 'system' WHERE owner_id IS NULL OR owner_id <> 'system'`,
	`CREATE INDEX IF NOT EXISTS idx_mail_domains_owner_id ON mail_domains(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_srv_mail_servers_owner_id ON srv_mail_servers(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_dkim_keys_owner_id ON mail_dkim_keys(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_spf_records_owner_id ON mail_spf_records(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_dmarc_policies_owner_id ON mail_dmarc_policies(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_pgp_keys_owner_id ON mail_pgp_keys(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_queue_log_owner_id ON mail_queue_log(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_delivery_log_owner_id ON mail_delivery_log(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_stats_hourly_owner_id ON mail_stats_hourly(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_stats_daily_owner_id ON mail_stats_daily(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_sieve_scripts_owner_id ON mail_sieve_scripts(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_archive_policies_owner_id ON mail_archive_policies(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_mail_archived_messages_owner_id ON mail_archived_messages(owner_id)`,
}
