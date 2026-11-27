package database

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidSecurityReportUpdate = errors.New("invalid security report update")
	ErrPGPKeyChanged               = errors.New("active PGP key changed while storing the security report")
)

// SecurityReportUpdate contains only operator-editable researcher-facing fields.
type SecurityReportUpdate struct {
	Status         string
	Comment        string
	DisclosureDate string
	Severity       string
}

// SecurityReport contains encrypted report material and non-sensitive status fields.
type SecurityReport struct {
	TrackingID        string
	IDWindow          int64
	Severity          string
	Component         string
	Status            string
	BodyEncrypted     []byte
	BodyAlgorithm     string
	PGPFingerprint    string
	ContactEncrypted  []byte
	CreditPreference  string
	CreditName        sql.NullString
	ResearcherComment string
	DisclosureDate    sql.NullString
	CVERequested      bool
	TokenHash         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ClosedAt          sql.NullTime
}

// SecurityReportStatus is the only report data returned to a researcher.
type SecurityReportStatus struct {
	Status            string
	Severity          string
	ResearcherComment string
	DisclosureDate    sql.NullString
	ClosedAt          sql.NullTime
}

// SecurityThanks contains a public researcher acknowledgment.
type SecurityThanks struct {
	Credit      string
	Severity    string
	DisclosedOn string
}

// SecurityReportSummary contains only fields permitted in the maintenance list.
type SecurityReportSummary struct {
	TrackingID string
	Status     string
	Severity   string
	Component  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SecurityReportDetails contains the safe metadata and researcher-facing comment shown in maintenance.
type SecurityReportDetails struct {
	SecurityReportSummary
	ResearcherComment string
	DisclosureDate    sql.NullString
}

// EncryptedSecurityReport exposes only the ciphertext required for a safe export.
type EncryptedSecurityReport struct {
	TrackingID string
	Algorithm  string
	Body       []byte
}

// DeleteClosedSecurityReports removes only reports that passed their configured closed-report retention period.
func (db *DB) DeleteClosedSecurityReports(ctx context.Context, retentionDays int, now time.Time) (int64, error) {
	if retentionDays < 1 {
		return 0, fmt.Errorf("security report retention must be at least one day")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cutoff := now.UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)
	var removed int64
	err := db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM security_reports WHERE closed_at IS NOT NULL AND closed_at < ?`, cutoff)
		if err != nil {
			return fmt.Errorf("delete expired closed security reports: %w", err)
		}
		removed, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count deleted security reports: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// ListSecurityReportSummaries returns report metadata without encrypted or plaintext content.
func (db *DB) ListSecurityReportSummaries(ctx context.Context, status string) ([]SecurityReportSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	query := `SELECT tracking_id, status, severity, component, created_at, updated_at FROM security_reports`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, tracking_id DESC`
	rows, err := db.ServerReadDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list security report summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]SecurityReportSummary, 0)
	for rows.Next() {
		var item SecurityReportSummary
		var createdAt, updatedAt string
		if err := rows.Scan(&item.TrackingID, &item.Status, &item.Severity, &item.Component, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("read security report summary: %w", err)
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse security report creation time: %w", err)
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse security report update time: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security report summaries: %w", err)
	}
	return items, nil
}

// GetSecurityReportDetails returns report metadata without decrypting or exposing report content.
func (db *DB) GetSecurityReportDetails(ctx context.Context, trackingID string) (SecurityReportDetails, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var item SecurityReportDetails
	var createdAt, updatedAt string
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT tracking_id, status, severity, component,
		researcher_comment, disclosure_date, created_at, updated_at FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(
		&item.TrackingID, &item.Status, &item.Severity, &item.Component, &item.ResearcherComment,
		&item.DisclosureDate, &createdAt, &updatedAt)
	if err != nil {
		return SecurityReportDetails{}, fmt.Errorf("get security report details: %w", err)
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return SecurityReportDetails{}, fmt.Errorf("parse security report creation time: %w", err)
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return SecurityReportDetails{}, fmt.Errorf("parse security report update time: %w", err)
	}
	return item, nil
}

// GetEncryptedSecurityReport returns stored ciphertext without decrypting it.
func (db *DB) GetEncryptedSecurityReport(ctx context.Context, trackingID string) (EncryptedSecurityReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var report EncryptedSecurityReport
	err := db.ServerReadDB.QueryRowContext(ctx, `SELECT tracking_id, body_alg, body_enc FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&report.TrackingID, &report.Algorithm, &report.Body)
	if err != nil {
		return EncryptedSecurityReport{}, fmt.Errorf("get encrypted security report: %w", err)
	}
	return report, nil
}

// UpdateSecurityReport applies forward-only triage changes and publishes an
// opted-in credit atomically when a report is disclosed.
func (db *DB) UpdateSecurityReport(ctx context.Context, trackingID string, update SecurityReportUpdate, now time.Time) (string, error) {
	if err := validateSecurityReportUpdate(update); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var oldStatus string
	err := db.Write(ctx, func(tx *sql.Tx) error {
		var currentSeverity, creditPreference string
		var creditName sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT status, severity, credit_pref, credit_name FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&oldStatus, &currentSeverity, &creditPreference, &creditName); err != nil {
			return err
		}
		newStatus := oldStatus
		if update.Status != "" {
			if !securityReportStatusCanAdvance(oldStatus, update.Status) {
				return ErrInvalidSecurityReportUpdate
			}
			newStatus = update.Status
		}
		newSeverity := currentSeverity
		if update.Severity != "" {
			newSeverity = update.Severity
		}
		updatedAt := now.UTC().Format(time.RFC3339Nano)
		_, err := tx.ExecContext(ctx, `UPDATE security_reports SET status = ?, severity = ?, researcher_comment = CASE WHEN ? = '' THEN researcher_comment ELSE ? END, disclosure_date = CASE WHEN ? = '' THEN disclosure_date ELSE ? END, updated_at = ?, closed_at = CASE WHEN ? IN ('disclosed', 'wont_fix') THEN COALESCE(closed_at, ?) ELSE closed_at END WHERE tracking_id = ?`,
			newStatus, newSeverity, update.Comment, update.Comment, update.DisclosureDate, update.DisclosureDate, updatedAt, newStatus, updatedAt, trackingID)
		if err != nil {
			return fmt.Errorf("update security report: %w", err)
		}
		if oldStatus != "disclosed" && newStatus == "disclosed" && creditPreference != "no" {
			credit := ""
			if creditPreference == "anon" {
				var count int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_thanks WHERE credit LIKE 'Anonymous Researcher #%'`).Scan(&count); err != nil {
					return fmt.Errorf("count anonymous security credits: %w", err)
				}
				credit = fmt.Sprintf("Anonymous Researcher #%d", count+1)
			} else if creditName.Valid {
				credit = strings.TrimSpace(creditName.String)
			}
			if credit != "" {
				_, err := tx.ExecContext(ctx, `INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on) VALUES (?, ?, ?, ?) ON CONFLICT(tracking_id) DO NOTHING`, trackingID, credit, newSeverity, now.UTC().Format("2006-01-02"))
				if err != nil {
					return fmt.Errorf("publish security researcher credit: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return oldStatus, err
	}
	return oldStatus, nil
}

func validateSecurityReportUpdate(update SecurityReportUpdate) error {
	if update.Status != "" && !validSecurityReportStatus(update.Status) ||
		update.Severity != "" && !validSecurityReportSeverity(update.Severity) ||
		len([]rune(update.Comment)) > 2000 {
		return ErrInvalidSecurityReportUpdate
	}
	if update.DisclosureDate != "" {
		if _, err := time.Parse("2006-01-02", update.DisclosureDate); err != nil {
			return ErrInvalidSecurityReportUpdate
		}
	}
	return nil
}

func validSecurityReportStatus(status string) bool {
	switch status {
	case "received", "triaged", "confirmed", "patching", "disclosed", "wont_fix":
		return true
	default:
		return false
	}
}

func validSecurityReportSeverity(severity string) bool {
	switch severity {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func securityReportStatusCanAdvance(current, next string) bool {
	if current == next {
		return true
	}
	if current == "disclosed" || current == "wont_fix" {
		return false
	}
	if next == "wont_fix" {
		return true
	}
	order := map[string]int{"received": 0, "triaged": 1, "confirmed": 2, "patching": 3, "disclosed": 4}
	currentOrder, currentOK := order[current]
	nextOrder, nextOK := order[next]
	return currentOK && nextOK && nextOrder > currentOrder
}

// CreateSecurityReport stores only encrypted report and contact bytes.
func (db *DB) CreateSecurityReport(ctx context.Context, report SecurityReport) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.Write(ctx, func(tx *sql.Tx) error {
		if report.BodyAlgorithm == "pgp" {
			var fingerprint string
			var revoked int
			if err := tx.QueryRowContext(ctx, `SELECT fingerprint, revoked FROM security_pgp_key_metadata WHERE id = 1`).Scan(&fingerprint, &revoked); err != nil {
				return fmt.Errorf("verify PGP key before storing security report: %w", err)
			}
			if revoked != 0 || report.PGPFingerprint == "" || fingerprint != report.PGPFingerprint {
				return ErrPGPKeyChanged
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO security_reports (
			tracking_id, id_window, severity, component, status, body_enc, body_alg,
			contact_enc, credit_pref, credit_name, cve_requested, token_hash, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'received', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			report.TrackingID, report.IDWindow, report.Severity, report.Component,
			report.BodyEncrypted, report.BodyAlgorithm, report.ContactEncrypted,
			report.CreditPreference, report.CreditName, report.CVERequested,
			report.TokenHash, report.CreatedAt.UTC().Format(time.RFC3339Nano),
			report.UpdatedAt.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("store security report: %w", err)
		}
		return nil
	})
}

// ConsumeSecurityReportStatus permits a matching researcher token once per UTC day.
func (db *DB) ConsumeSecurityReportStatus(ctx context.Context, trackingID, tokenHash, today string, now time.Time) (SecurityReportStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var status SecurityReportStatus
	err := db.Write(ctx, func(tx *sql.Tx) error {
		var storedHash string
		var lastUsed sql.NullString
		var closed sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT token_hash, token_last_used, closed_at FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&storedHash, &lastUsed, &closed); err != nil {
			return err
		}
		if len(storedHash) != len(tokenHash) || subtle.ConstantTimeCompare([]byte(storedHash), []byte(tokenHash)) != 1 {
			return sql.ErrNoRows
		}
		if lastUsed.Valid && lastUsed.String >= today {
			return sql.ErrNoRows
		}
		if closed.Valid {
			closedAt, parseErr := time.Parse(time.RFC3339Nano, closed.String)
			if parseErr != nil || !closedAt.After(now.UTC().Add(-30*24*time.Hour)) {
				return sql.ErrNoRows
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE security_reports SET token_last_used = ?
			WHERE tracking_id = ? AND (token_last_used IS NULL OR token_last_used < ?)`, today, trackingID, today)
		if err != nil {
			return fmt.Errorf("consume security report status token: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check security report status token: %w", err)
		}
		if affected != 1 {
			return sql.ErrNoRows
		}
		var disclosure sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT status, severity, researcher_comment, disclosure_date, closed_at
			FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(
			&status.Status, &status.Severity, &status.ResearcherComment, &disclosure, &closed); err != nil {
			return err
		}
		status.DisclosureDate = disclosure
		if closed.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, closed.String)
			if parseErr == nil {
				status.ClosedAt = sql.NullTime{Time: parsed, Valid: true}
			}
		}
		return nil
	})
	if err != nil {
		return SecurityReportStatus{}, err
	}
	return status, nil
}

// ListSecurityThanks returns public researcher credits without report data.
func (db *DB) ListSecurityThanks(ctx context.Context) ([]SecurityThanks, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.ServerReadDB.QueryContext(ctx, `SELECT credit, severity, disclosed_on
		FROM security_thanks ORDER BY disclosed_on DESC, tracking_id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list security acknowledgments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]SecurityThanks, 0)
	for rows.Next() {
		var item SecurityThanks
		if err := rows.Scan(&item.Credit, &item.Severity, &item.DisclosedOn); err != nil {
			return nil, fmt.Errorf("read security acknowledgment: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security acknowledgments: %w", err)
	}
	return items, nil
}
