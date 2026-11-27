package database

// serverCreateStatements contains all CREATE TABLE statements for server.db
// Per AI.md PART 10: All tables use CREATE TABLE IF NOT EXISTS (idempotent)
var serverCreateStatements = []string{
	`CREATE TABLE IF NOT EXISTS app_secrets (
		name TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,

	// Rate limiting - sliding window counters
	`CREATE TABLE IF NOT EXISTS rate_limits (
		key         TEXT PRIMARY KEY,
		count       INTEGER NOT NULL DEFAULT 1,
		prev_count  INTEGER NOT NULL DEFAULT 0,
		window_start INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_rate_limits_window ON rate_limits(window_start)`,

	`CREATE TABLE IF NOT EXISTS captcha_challenges (
		id          VARCHAR(32) PRIMARY KEY,
		answer_hash VARCHAR(64) NOT NULL,
		expires_at  TIMESTAMP NOT NULL
	)`,

	`CREATE INDEX IF NOT EXISTS idx_captcha_challenges_expires ON captcha_challenges(expires_at)`,
	`CREATE TABLE IF NOT EXISTS contact_review_tokens (
		nonce_hash TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_contact_review_tokens_expires ON contact_review_tokens(expires_at)`,

	`CREATE TABLE IF NOT EXISTS security_reports (
		tracking_id TEXT PRIMARY KEY,
		id_window INTEGER NOT NULL,
		severity TEXT NOT NULL,
		component TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'received',
		body_enc BLOB NOT NULL,
		body_alg TEXT NOT NULL,
		contact_enc BLOB,
		credit_pref TEXT NOT NULL DEFAULT 'no',
		credit_name TEXT,
		researcher_comment TEXT NOT NULL DEFAULT '',
		disclosure_date TEXT,
		cve_requested INTEGER NOT NULL DEFAULT 0,
		token_hash TEXT NOT NULL,
		token_last_used TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		closed_at TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_security_reports_status ON security_reports(status, created_at)`,
	`CREATE TABLE IF NOT EXISTS security_pgp_key_metadata (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		fingerprint TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		last_rotated_at TEXT,
		keyservers_published TEXT NOT NULL DEFAULT '[]',
		revoked INTEGER NOT NULL DEFAULT 0 CHECK (revoked IN (0, 1))
	)`,
	`CREATE TABLE IF NOT EXISTS security_thanks (
		tracking_id TEXT PRIMARY KEY,
		credit TEXT NOT NULL,
		severity TEXT NOT NULL,
		disclosed_on TEXT NOT NULL
	)`,

	// Audit log - admin actions, config changes, security events
	`CREATE TABLE IF NOT EXISTS audit_log (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp   INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		level       TEXT NOT NULL DEFAULT 'info',
		category    TEXT NOT NULL,
		action      TEXT NOT NULL,
		actor_type  TEXT,
		actor_id    TEXT,
		actor_ip    TEXT,
		target_type TEXT,
		target_id   TEXT,
		details     TEXT,
		success     INTEGER NOT NULL DEFAULT 1
	)`,

	`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_category ON audit_log(category)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_type, actor_id)`,

	`CREATE TABLE IF NOT EXISTS owners (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		last_seen_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	)`,

	`CREATE TABLE IF NOT EXISTS api_tokens (
		id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK (kind IN ('primary', 'linked')),
		name TEXT,
		token_hash TEXT NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		token_last4 TEXT NOT NULL,
		token_enc TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		rotated_at INTEGER,
		expires_at INTEGER,
		last_used_at INTEGER,
		revoked_at INTEGER,
		revoked_reason TEXT
	)`,

	`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_primary ON api_tokens(owner_id) WHERE kind = 'primary' AND revoked_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_prefix ON api_tokens(token_prefix)`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_owner ON api_tokens(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_active ON api_tokens(revoked_at) WHERE revoked_at IS NULL`,

	// Scheduler tasks - background task definitions
	`CREATE TABLE IF NOT EXISTS scheduler_tasks (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		task_type   TEXT NOT NULL DEFAULT 'global',
		enabled     INTEGER NOT NULL DEFAULT 1,
		schedule    TEXT NOT NULL,
		last_run    INTEGER,
		next_run    INTEGER,
		last_status TEXT,
		last_error  TEXT,
		run_count   INTEGER NOT NULL DEFAULT 0,
		fail_count  INTEGER NOT NULL DEFAULT 0,
		locked_by   TEXT,
		locked_at   INTEGER
	)`,

	// Scheduler history - task execution history
	`CREATE TABLE IF NOT EXISTS scheduler_history (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id     TEXT NOT NULL,
		started_at  INTEGER NOT NULL,
		finished_at INTEGER,
		status      TEXT NOT NULL,
		error       TEXT,
		duration_ms INTEGER
	)`,

	`CREATE INDEX IF NOT EXISTS idx_scheduler_history_task ON scheduler_history(task_id)`,
	`CREATE INDEX IF NOT EXISTS idx_scheduler_history_started ON scheduler_history(started_at)`,

	// Backups - backup history and metadata
	`CREATE TABLE IF NOT EXISTS backups (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		filename    TEXT NOT NULL UNIQUE,
		filepath    TEXT NOT NULL,
		size_bytes  INTEGER NOT NULL,
		type        TEXT NOT NULL DEFAULT 'auto',
		created_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		checksum    TEXT,
		notes       TEXT
	)`,

	`CREATE INDEX IF NOT EXISTS idx_backups_created ON backups(created_at)`,

	`CREATE TABLE IF NOT EXISTS request_stats (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		total_requests INTEGER NOT NULL DEFAULT 0
	)`,
	`INSERT OR IGNORE INTO request_stats (id, total_requests) VALUES (1, 0)`,
	`CREATE TABLE IF NOT EXISTS request_stats_hourly (
		hour_start INTEGER PRIMARY KEY,
		request_count INTEGER NOT NULL DEFAULT 0
	)`,
}

// serverSchemaUpdates contains idempotent schema updates for server.db
// Per AI.md PART 10: Add new columns/indexes here, safe to run multiple times
var serverSchemaUpdates = []string{
	// Version: add the previous-window value used by the sliding rate-limit counter.
	`ALTER TABLE rate_limits ADD COLUMN prev_count INTEGER NOT NULL DEFAULT 0`,
}
