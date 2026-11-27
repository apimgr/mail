package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MailDomain is the persisted virtual-domain configuration without key material.
type MailDomain struct {
	ID           string
	Domain       string
	Description  string
	OwnerType    string
	OwnerID      string
	MaxMailboxes int64
	MaxAliases   int64
	MaxQuotaMB   int64
	Enabled      bool
	CreatedAt    int64
	UpdatedAt    int64
}

// MailDomainInput contains the operator-editable fields for a virtual domain.
type MailDomainInput struct {
	Domain       string
	Description  string
	MaxMailboxes int64
	MaxAliases   int64
	MaxQuotaMB   int64
	Enabled      bool
}

// MailDKIMKey contains key material for operator-side signing and its public DNS value.
type MailDKIMKey struct {
	Selector   string
	PrivateKey string
	PublicKey  string
}

// MailDNSRecord is public DNS data assembled only from stored mail configuration.
type MailDNSRecord struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Preference *int   `json:"preference,omitempty"`
}

// ListMailDomains returns domain metadata without reading any DKIM key tables.
func (db *DB) ListMailDomains(ctx context.Context, page, limit int) ([]MailDomain, int64, error) {
	if page < 1 || limit < 1 || limit > 250 || page > int(^uint(0)>>1)/limit {
		return nil, 0, fmt.Errorf("mail domain pagination is outside the allowed range")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var total int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_domains`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count mail domains: %w", err)
	}
	query := fmt.Sprintf(`SELECT public_id, domain, COALESCE(description, ''), owner_type, %s,
		max_mailboxes, max_aliases, max_quota_mb, enabled, created_at, updated_at
		FROM mail_domains ORDER BY domain LIMIT ? OFFSET ?`, db.mailDomainOwnerColumn())
	rows, err := db.UsersReadDB.QueryContext(ctx, query, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list mail domains: %w", err)
	}
	defer func() { _ = rows.Close() }()
	domains := make([]MailDomain, 0)
	for rows.Next() {
		var domain MailDomain
		var enabled int
		if err := rows.Scan(&domain.ID, &domain.Domain, &domain.Description, &domain.OwnerType, &domain.OwnerID,
			&domain.MaxMailboxes, &domain.MaxAliases, &domain.MaxQuotaMB, &enabled, &domain.CreatedAt, &domain.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("read mail domain: %w", err)
		}
		domain.Enabled = enabled != 0
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mail domains: %w", err)
	}
	return domains, total, nil
}

// GetMailDomain returns one domain's metadata without reading any DKIM key tables.
func (db *DB) GetMailDomain(ctx context.Context, name string) (MailDomain, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var domain MailDomain
	var enabled int
	query := fmt.Sprintf(`SELECT public_id, domain, COALESCE(description, ''), owner_type, %s,
		max_mailboxes, max_aliases, max_quota_mb, enabled, created_at, updated_at
		FROM mail_domains WHERE domain = ?`, db.mailDomainOwnerColumn())
	err := db.UsersReadDB.QueryRowContext(ctx, query, name).Scan(&domain.ID, &domain.Domain, &domain.Description, &domain.OwnerType,
		&domain.OwnerID, &domain.MaxMailboxes, &domain.MaxAliases, &domain.MaxQuotaMB, &enabled, &domain.CreatedAt, &domain.UpdatedAt)
	if err != nil {
		return MailDomain{}, fmt.Errorf("get mail domain: %w", err)
	}
	domain.Enabled = enabled != 0
	return domain, nil
}

// CreateMailDomain persists a system-owned virtual domain and returns its public metadata.
func (db *DB) CreateMailDomain(ctx context.Context, input MailDomainInput) (MailDomain, error) {
	publicID, err := uuid.NewV7()
	if err != nil {
		return MailDomain{}, fmt.Errorf("generate mail domain public ID: %w", err)
	}
	now := time.Now().UTC().Unix()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		if db.mailDomainOwnerRef {
			_, err := tx.ExecContext(ctx, `INSERT INTO mail_domains
				(public_id, domain, description, owner_type, owner_ref, owner_id, max_mailboxes, max_aliases, max_quota_mb, enabled, created_at, updated_at)
				VALUES (?, ?, ?, 'system', 'system', 0, ?, ?, ?, ?, ?, ?)`, publicID.String(), input.Domain, input.Description,
				input.MaxMailboxes, input.MaxAliases, input.MaxQuotaMB, input.Enabled, now, now)
			return err
		}
		query := fmt.Sprintf(`INSERT INTO mail_domains
			(public_id, domain, description, owner_type, %s, max_mailboxes, max_aliases, max_quota_mb, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 'system', 'system', ?, ?, ?, ?, ?, ?)`, db.mailDomainOwnerColumn())
		_, err := tx.ExecContext(ctx, query, publicID.String(), input.Domain, input.Description,
			input.MaxMailboxes, input.MaxAliases, input.MaxQuotaMB, input.Enabled, now, now)
		return err
	})
	if err != nil {
		return MailDomain{}, fmt.Errorf("create mail domain: %w", err)
	}
	return db.GetMailDomain(ctx, input.Domain)
}

// UpdateMailDomain replaces operator-editable metadata for an existing domain.
func (db *DB) UpdateMailDomain(ctx context.Context, name string, input MailDomainInput) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE mail_domains SET domain = ?, description = ?, max_mailboxes = ?,
			max_aliases = ?, max_quota_mb = ?, enabled = ?, updated_at = ? WHERE domain = ?`, input.Domain, input.Description,
			input.MaxMailboxes, input.MaxAliases, input.MaxQuotaMB, input.Enabled, time.Now().UTC().Unix(), name)
		if err != nil {
			return fmt.Errorf("update mail domain: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check updated mail domain: %w", err)
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		if name != input.Domain {
			rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(primary_for_domains, ''), COALESCE(relay_domains, '') FROM srv_mail_servers`)
			if err != nil {
				return fmt.Errorf("list mail server domain mappings: %w", err)
			}
			type serverMapping struct {
				id             int64
				primary, relay string
			}
			var mappings []serverMapping
			for rows.Next() {
				var mapping serverMapping
				if err := rows.Scan(&mapping.id, &mapping.primary, &mapping.relay); err != nil {
					_ = rows.Close()
					return fmt.Errorf("read mail server domain mappings: %w", err)
				}
				mappings = append(mappings, mapping)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterate mail server domain mappings: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close mail server domain mappings: %w", err)
			}
			for _, mapping := range mappings {
				primary, primaryChanged := replaceDomainMapping(mapping.primary, name, input.Domain)
				relay, relayChanged := replaceDomainMapping(mapping.relay, name, input.Domain)
				if primaryChanged || relayChanged {
					if _, err := tx.ExecContext(ctx, `UPDATE srv_mail_servers SET primary_for_domains = ?, relay_domains = ? WHERE id = ?`, primary, relay, mapping.id); err != nil {
						return fmt.Errorf("update mail server domain mappings: %w", err)
					}
				}
			}
		}
		return nil
	})
}

func replaceDomainMapping(value, oldDomain, newDomain string) (string, bool) {
	var output strings.Builder
	start := 0
	changed := false
	for index := 0; index <= len(value); index++ {
		if index < len(value) && !isDomainMappingDelimiter(value[index]) {
			continue
		}
		item := value[start:index]
		if strings.EqualFold(strings.TrimSuffix(item, "."), oldDomain) {
			output.WriteString(newDomain)
			changed = true
		} else {
			output.WriteString(item)
		}
		if index < len(value) {
			output.WriteByte(value[index])
		}
		start = index + 1
	}
	if !changed {
		return value, false
	}
	return output.String(), true
}

func isDomainMappingDelimiter(value byte) bool {
	return value == ',' || value == ';' || value == ' ' || value == '\n' || value == '\t'
}

// DeleteMailDomain removes a virtual domain only when it has no dependent resources.
func (db *DB) DeleteMailDomain(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		var domainID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mail_domains WHERE domain = ?`, name).Scan(&domainID); err != nil {
			return err
		}
		for _, table := range []string{"mail_mailboxes", "mail_aliases", "mail_dkim_keys", "mail_spf_records", "mail_dmarc_policies", "mail_archive_policies"} {
			query := "SELECT EXISTS(SELECT 1 FROM " + table + " WHERE domain_id = ?)"
			var exists bool
			if err := tx.QueryRowContext(ctx, query, domainID).Scan(&exists); err != nil {
				return fmt.Errorf("check domain dependencies: %w", err)
			}
			if exists {
				return ErrMailDomainInUse
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT COALESCE(primary_for_domains, ''), COALESCE(relay_domains, '') FROM srv_mail_servers`)
		if err != nil {
			return fmt.Errorf("check mail server domain mappings: %w", err)
		}
		for rows.Next() {
			var primary, relay string
			if err := rows.Scan(&primary, &relay); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read mail server domain mappings: %w", err)
			}
			if domainMappingContains(primary, name) || domainMappingContains(relay, name) {
				_ = rows.Close()
				return ErrMailDomainInUse
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate mail server domain mappings: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close mail server domain mappings: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mail_domains WHERE id = ?`, domainID); err != nil {
			return fmt.Errorf("delete mail domain: %w", err)
		}
		return nil
	})
}

func domainMappingContains(value, domain string) bool {
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		if strings.EqualFold(strings.TrimSuffix(item, "."), domain) {
			return true
		}
	}
	return false
}

// ErrMailDomainInUse indicates that child mail resources still reference a domain.
var ErrMailDomainInUse = errors.New("mail domain has dependent resources")

// IsMailDomainConflict reports a uniqueness conflict from a domain write.
func IsMailDomainConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate")
}

// SaveMailDKIMKey persists private and public key material for a domain selector.
func (db *DB) SaveMailDKIMKey(ctx context.Context, domain, selector, privateKey, publicKey string) error {
	if !strings.HasPrefix(privateKey, "enc:v1:") {
		return errors.New("DKIM private key must use the versioned encrypted format")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		var domainID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mail_domains WHERE domain = ?`, domain).Scan(&domainID); err != nil {
			return fmt.Errorf("find mail domain for DKIM key: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mail_dkim_keys
			(domain_id, selector, private_key, public_key, key_size, enabled) VALUES (?, ?, ?, ?, 2048, 1)`, domainID, selector, privateKey, publicKey); err != nil {
			return fmt.Errorf("save mail DKIM key: %w", err)
		}
		return nil
	})
}

// ListMailDKIMKeys returns public DKIM data without reading private key material.
func (db *DB) ListMailDKIMKeys(ctx context.Context, domain string) ([]MailDKIMKey, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT k.selector, k.public_key FROM mail_dkim_keys AS k
		JOIN mail_domains AS d ON d.id = k.domain_id WHERE d.domain = ? AND k.enabled = 1 ORDER BY k.selector`, domain)
	if err != nil {
		return nil, fmt.Errorf("list public mail DKIM keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys := make([]MailDKIMKey, 0)
	for rows.Next() {
		var key MailDKIMKey
		if err := rows.Scan(&key.Selector, &key.PublicKey); err != nil {
			return nil, fmt.Errorf("read public mail DKIM key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public mail DKIM keys: %w", err)
	}
	return keys, nil
}

// GetMailDNSRecords returns DNS records backed by current domain and server configuration.
func (db *DB) GetMailDNSRecords(ctx context.Context, domain string) ([]MailDNSRecord, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var domainID int64
	if err := db.UsersReadDB.QueryRowContext(ctx, `SELECT id FROM mail_domains WHERE domain = ?`, domain).Scan(&domainID); err != nil {
		return nil, nil, fmt.Errorf("find mail domain for DNS records: %w", err)
	}
	records := make([]MailDNSRecord, 0)
	missing := make([]string, 0, 4)

	mailServers, err := db.UsersReadDB.QueryContext(ctx, `SELECT hostname, COALESCE(primary_for_domains, ''), mx_priority FROM srv_mail_servers WHERE enabled = 1`)
	if err != nil {
		return nil, nil, fmt.Errorf("list mail servers for DNS records: %w", err)
	}
	for mailServers.Next() {
		var hostname, domains string
		var preference int
		if err := mailServers.Scan(&hostname, &domains, &preference); err != nil {
			_ = mailServers.Close()
			return nil, nil, fmt.Errorf("read mail server DNS mapping: %w", err)
		}
		for _, configuredDomain := range strings.FieldsFunc(domains, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
			if strings.EqualFold(strings.TrimSuffix(configuredDomain, "."), domain) && hostname != "" {
				records = append(records, MailDNSRecord{Type: "MX", Name: domain, Value: hostname, Preference: &preference})
				break
			}
		}
	}
	if err := mailServers.Err(); err != nil {
		_ = mailServers.Close()
		return nil, nil, fmt.Errorf("iterate mail server DNS mappings: %w", err)
	}
	if err := mailServers.Close(); err != nil {
		return nil, nil, fmt.Errorf("close mail server DNS mappings: %w", err)
	}
	if !hasDNSRecord(records, "MX") {
		missing = append(missing, "MX")
	}

	var spf string
	err = db.UsersReadDB.QueryRowContext(ctx, `SELECT policy FROM mail_spf_records WHERE domain_id = ? AND enabled = 1`, domainID).Scan(&spf)
	if err == nil && strings.TrimSpace(spf) != "" {
		records = append(records, MailDNSRecord{Type: "TXT", Name: domain, Value: strings.TrimSpace(spf)})
	} else if errors.Is(err, sql.ErrNoRows) || (err == nil && strings.TrimSpace(spf) == "") {
		missing = append(missing, "SPF")
	} else {
		return nil, nil, fmt.Errorf("read SPF DNS record: %w", err)
	}

	var policy, subdomainPolicy, rua string
	var percentage int
	err = db.UsersReadDB.QueryRowContext(ctx, `SELECT policy, COALESCE(subdomain_policy, ''), percentage, COALESCE(rua_email, '')
		FROM mail_dmarc_policies WHERE domain_id = ? AND enabled = 1`, domainID).Scan(&policy, &subdomainPolicy, &percentage, &rua)
	if err == nil {
		value := fmt.Sprintf("v=DMARC1; p=%s; pct=%d", strings.TrimSpace(policy), percentage)
		if subdomainPolicy = strings.TrimSpace(subdomainPolicy); subdomainPolicy != "" {
			value += "; sp=" + subdomainPolicy
		}
		if rua = strings.TrimSpace(rua); rua != "" {
			value += "; rua=mailto:" + rua
		}
		records = append(records, MailDNSRecord{Type: "TXT", Name: "_dmarc." + domain, Value: value})
	} else if errors.Is(err, sql.ErrNoRows) {
		missing = append(missing, "DMARC")
	} else {
		return nil, nil, fmt.Errorf("read DMARC DNS record: %w", err)
	}

	keys, err := db.ListMailDKIMKeys(ctx, domain)
	if err != nil {
		return nil, nil, err
	}
	for _, key := range keys {
		records = append(records, MailDNSRecord{
			Type: "TXT", Name: key.Selector + "._domainkey." + domain,
			Value: "v=DKIM1; k=rsa; p=" + key.PublicKey,
		})
	}
	if len(keys) == 0 {
		missing = append(missing, "DKIM")
	}
	return records, missing, nil
}

func hasDNSRecord(records []MailDNSRecord, recordType string) bool {
	for _, record := range records {
		if record.Type == recordType {
			return true
		}
	}
	return false
}

func (db *DB) backfillMailDomainPublicIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.UsersReadDB.QueryContext(ctx, `SELECT id FROM mail_domains WHERE public_id IS NULL OR public_id = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list mail domains missing public IDs: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read mail domain ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate mail domain IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close mail domain ID rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithTransaction(ctx, db.UsersDB, func(tx *sql.Tx) error {
		for _, id := range ids {
			publicID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generate mail domain public ID: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_domains SET public_id = ? WHERE id = ? AND (public_id IS NULL OR public_id = '')`, publicID.String(), id); err != nil {
				return fmt.Errorf("backfill mail domain public ID: %w", err)
			}
		}
		return nil
	})
}

// IsMailDomainNotFound reports whether a domain lookup returned no row.
func IsMailDomainNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
