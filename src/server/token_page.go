package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
)

const (
	ownerTokenCookieName = "mail_owner_token_aqhqfbeq"
	tokenViewCookieName  = "mail_token_view"
	tokenPagePath        = "/server/preferences/tokens"
)

type tokenPageView struct {
	Authenticated bool
	Primary       tokenPageToken
	Linked        []tokenPageToken
	MaxLinked     int
	LinkedCount   int
	ResourceCount int64
	TokenLast4    string
	ErrorKey      string
	NoticeKey     string
}

type tokenPageToken struct {
	ID       string
	Name     string
	Masked   string
	Raw      string
	Status   string
	Created  string
	LastUsed string
	Expires  string
}

func ownerCookieMaxAge(value string) int {
	unit := time.Second
	value = strings.TrimSpace(value)
	for suffix, duration := range map[string]time.Duration{"y": 365 * 24 * time.Hour, "w": 7 * 24 * time.Hour, "d": 24 * time.Hour} {
		if strings.HasSuffix(value, suffix) {
			unit = duration
			value = strings.TrimSuffix(value, suffix)
			break
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 365 * 24 * 60 * 60
	}
	seconds := int(time.Duration(n) * unit / time.Second)
	if seconds < 1 {
		return 365 * 24 * 60 * 60
	}
	return seconds
}

func (s *Server) setOwnerCookie(w http.ResponseWriter, r *http.Request, token string, clear bool) {
	maxAge := ownerCookieMaxAge(s.cfg.Server.Tokens.CookieMaxAge)
	if clear {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: ownerTokenCookieName, Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func (s *Server) setTokenViewCookie(w http.ResponseWriter, r *http.Request, card string) {
	value, ok := s.signTokenViewCookie(r.Context(), card)
	if !ok {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: tokenViewCookieName, Value: value, Path: tokenPagePath, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 60})
}

func (s *Server) signTokenViewCookie(ctx context.Context, card string) (string, bool) {
	if s.db == nil {
		return "", false
	}
	key, err := s.db.GeneratedSecret(ctx, "cookie_signing_key")
	if err != nil || len(key) == 0 {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("token-view:" + card))
	return card + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), true
}

func (s *Server) verifyTokenViewCookie(ctx context.Context, value string) (string, bool) {
	card, signature, ok := strings.Cut(value, ".")
	if !ok || card == "" || signature == "" {
		return "", false
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return "", false
	}
	expectedValue, ok := s.signTokenViewCookie(ctx, card)
	if !ok {
		return "", false
	}
	_, expectedSignature, ok := strings.Cut(expectedValue, ".")
	if !ok {
		return "", false
	}
	expected, err := base64.RawURLEncoding.DecodeString(expectedSignature)
	if err != nil || len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
		return "", false
	}
	return card, true
}

func (s *Server) clearTokenViewCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: tokenViewCookieName, Value: "", Path: tokenPagePath, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (s *Server) ownerCookieIdentity(r *http.Request) (database.OwnerTokenIdentity, string, bool, error) {
	cookie, err := r.Cookie(ownerTokenCookieName)
	if err != nil || !validOwnerToken(cookie.Value) || s.db == nil || s.db.ServerDB == nil {
		return database.OwnerTokenIdentity{}, "", false, nil
	}
	identity, valid, err := s.db.ResolveOwnerToken(r.Context(), cookie.Value[:12], hashOwnerToken(cookie.Value))
	if err != nil {
		return database.OwnerTokenIdentity{}, "", false, err
	}
	if !valid || identity.Kind != "primary" {
		return database.OwnerTokenIdentity{}, cookie.Value, false, nil
	}
	return identity, cookie.Value, true, nil
}

func (s *Server) handleOwnerTokensPage(w http.ResponseWriter, r *http.Request) {
	page := &tokenPageView{MaxLinked: s.cfg.Server.Tokens.MaxLinked, ErrorKey: tokenNoticeKey(r.URL.Query().Get("error")), NoticeKey: tokenNoticeKey(r.URL.Query().Get("notice"))}
	if page.MaxLinked < 1 {
		page.MaxLinked = 25
	}
	identity, token, valid, err := s.ownerCookieIdentity(r)
	if err != nil {
		page.ErrorKey = "tokens.error.unavailable"
	}
	if !valid {
		if err == nil {
			if _, cookieErr := r.Cookie(ownerTokenCookieName); cookieErr == nil {
				s.setOwnerCookie(w, r, "", true)
			}
		}
		s.clearTokenViewCookie(w, r)
		s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "tokens.title", DescriptionKey: "tokens.lead", BreadcrumbKey: "nav.preferences", BreadcrumbPath: "/server/preferences", NoIndex: true, Tokens: page})
		return
	}
	s.setOwnerCookie(w, r, token, false)
	page.Authenticated = true
	records, err := s.db.ListOwnerTokens(r.Context(), identity.OwnerID)
	if err != nil {
		page.ErrorKey = "tokens.error.unavailable"
	} else {
		for _, record := range records {
			item := tokenPageToken{ID: record.ID, Name: record.Name.String, Masked: "own_" + strings.Repeat("•", 24) + record.TokenLast4, Status: linkedTokenStatus(record, time.Now()), Created: time.Unix(record.CreatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")}
			if record.LastUsedAt.Valid {
				item.LastUsed = time.Unix(record.LastUsedAt.Int64, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			}
			if record.ExpiresAt.Valid {
				item.Expires = time.Unix(record.ExpiresAt.Int64, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			}
			if record.Kind == "primary" {
				page.Primary = item
				page.TokenLast4 = record.TokenLast4
			} else {
				page.Linked = append(page.Linked, item)
			}
		}
		page.LinkedCount = len(page.Linked)
		page.ResourceCount, err = countOwnerResources(r.Context(), s.db.ServerReadDB, identity.OwnerID)
		if err != nil {
			page.ErrorKey = "tokens.error.unavailable"
		}
	}
	if view, cookieErr := r.Cookie(tokenViewCookieName); cookieErr == nil {
		s.clearTokenViewCookie(w, r)
		card, signed := s.verifyTokenViewCookie(r.Context(), view.Value)
		if !signed {
			view.Value = ""
		} else {
			view.Value = card
		}
		var ciphertext string
		var target *tokenPageToken
		if view.Value == "primary" {
			target = &page.Primary
			ciphertext, err = s.db.OwnerPrimaryTokenCiphertext(r.Context(), identity.OwnerID)
		} else {
			for i := range page.Linked {
				if page.Linked[i].ID == view.Value && page.Linked[i].Status == "active" {
					target = &page.Linked[i]
					ciphertext, err = s.db.LinkedTokenCiphertext(r.Context(), identity.OwnerID, view.Value)
					break
				}
			}
		}
		if target != nil && err == nil {
			target.Raw, err = decryptOwnerToken(ciphertext, s.cfg.Server.Security.EncryptionKey)
		}
		if target != nil && err != nil {
			page.ErrorKey = "tokens.error.unavailable"
		}
	}
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "tokens.title", DescriptionKey: "tokens.lead", BreadcrumbKey: "nav.preferences", BreadcrumbPath: "/server/preferences", NoIndex: true, Tokens: page})
}

func tokenNoticeKey(value string) string {
	switch value {
	case "restore":
		return "tokens.notice.restore"
	case "created":
		return "tokens.notice.created"
	case "rotated":
		return "tokens.notice.rotated"
	case "updated":
		return "tokens.notice.updated"
	case "revoked":
		return "tokens.notice.revoked"
	case "forgot":
		return "tokens.notice.forgot"
	case "unavailable":
		return "tokens.error.unavailable"
	case "invalid":
		return "tokens.error.invalid"
	case "conflict":
		return "tokens.error.conflict"
	case "validation":
		return "tokens.error.validation"
	default:
		return ""
	}
}

func (s *Server) requireOwnerCookie(w http.ResponseWriter, r *http.Request, limited bool) (database.OwnerTokenIdentity, string, bool) {
	if limited && !s.enforceStrictRateLimit(w, r) {
		return database.OwnerTokenIdentity{}, "", false
	}
	identity, token, ok, err := s.ownerCookieIdentity(r)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return database.OwnerTokenIdentity{}, "", false
	}
	if !ok {
		s.setOwnerCookie(w, r, "", true)
		http.Redirect(w, r, tokenPagePath+"?notice=restore", http.StatusSeeOther)
		return database.OwnerTokenIdentity{}, "", false
	}
	s.setOwnerCookie(w, r, token, false)
	return identity, token, true
}

func (s *Server) handleWebIssuePrimaryToken(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	valid, err := s.hasValidBearerToken(r)
	_, _, cookieValid, cookieErr := s.ownerCookieIdentity(r)
	if err != nil || cookieErr != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	if valid || cookieValid {
		http.Redirect(w, r, tokenPagePath+"?error=conflict", http.StatusSeeOther)
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	encrypted, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	ownerID, err := s.db.CreateOwnerPrimaryToken(r.Context(), hashOwnerToken(token), token[:12], token[len(token)-4:], encrypted)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.created", "owner", map[string]interface{}{"owner_id": ownerID, "token_prefix": token[:12], "source": "issue"})
	s.setOwnerCookie(w, r, token, false)
	s.setTokenViewCookie(w, r, "primary")
	http.Redirect(w, r, tokenPagePath+"#primary", http.StatusSeeOther)
}

func (s *Server) handleWebRestorePrimaryToken(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	token := strings.TrimSpace(r.PostForm.Get("token"))
	if !validOwnerToken(token) {
		http.Redirect(w, r, tokenPagePath+"?error=invalid", http.StatusSeeOther)
		return
	}
	identity, valid, err := s.db.ResolveOwnerToken(r.Context(), token[:12], hashOwnerToken(token))
	if err != nil || !valid || identity.Kind != "primary" {
		http.Redirect(w, r, tokenPagePath+"?error=invalid", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.restored", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "web"})
	s.setOwnerCookie(w, r, token, false)
	http.Redirect(w, r, tokenPagePath, http.StatusSeeOther)
}

func (s *Server) handleWebForgetPrimaryToken(w http.ResponseWriter, r *http.Request) {
	s.setOwnerCookie(w, r, "", true)
	w.Header().Set("Clear-Site-Data", `"storage"`)
	http.Redirect(w, r, tokenPagePath+"?notice=forgot", http.StatusSeeOther)
}

func (s *Server) handleWebRevealPrimaryToken(w http.ResponseWriter, r *http.Request) {
	identity, _, ok := s.requireOwnerCookie(w, r, true)
	if !ok {
		return
	}
	ciphertext, err := s.db.OwnerPrimaryTokenCiphertext(r.Context(), identity.OwnerID)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	token, err := decryptOwnerToken(ciphertext, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.revealed", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "web"})
	s.setTokenViewCookie(w, r, "primary")
	http.Redirect(w, r, tokenPagePath+"#primary", http.StatusSeeOther)
}

func (s *Server) handleWebRotatePrimaryToken(w http.ResponseWriter, r *http.Request) {
	identity, _, ok := s.requireOwnerCookie(w, r, true)
	if !ok {
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	encrypted, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	err = s.db.Write(r.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(r.Context(), `UPDATE api_tokens SET token_hash=?,token_prefix=?,token_last4=?,token_enc=?,rotated_at=? WHERE owner_id=? AND kind='primary' AND revoked_at IS NULL`, hashOwnerToken(token), token[:12], token[len(token)-4:], encrypted, time.Now().UTC().Unix(), identity.OwnerID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err == nil && changed != 1 {
			return errors.New("primary token missing")
		}
		return err
	})
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.rotated", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "web"})
	s.setOwnerCookie(w, r, token, false)
	s.setTokenViewCookie(w, r, "primary")
	http.Redirect(w, r, tokenPagePath+"?notice=rotated#primary", http.StatusSeeOther)
}

func (s *Server) handleWebRevokePrimaryToken(w http.ResponseWriter, r *http.Request) {
	identity, _, ok := s.requireOwnerCookie(w, r, true)
	if !ok {
		return
	}
	records, err := s.db.ListOwnerTokens(r.Context(), identity.OwnerID)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	last4, prefix := "", ""
	for _, record := range records {
		if record.Kind == "primary" {
			last4, prefix = record.TokenLast4, record.TokenPrefix
		}
	}
	confirm := strings.TrimSpace(r.PostForm.Get("confirm"))
	if len(confirm) != len(last4) || subtle.ConstantTimeCompare([]byte(confirm), []byte(last4)) != 1 {
		http.Redirect(w, r, tokenPagePath+"?error=validation#revoke-primary", http.StatusSeeOther)
		return
	}
	resources, err := countOwnerResources(r.Context(), s.db.ServerReadDB, identity.OwnerID)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	err = s.db.Write(r.Context(), func(tx *sql.Tx) error {
		tables, err := ownerResourceTables(tx)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if _, err := tx.ExecContext(r.Context(), `DELETE FROM "`+strings.ReplaceAll(table, `"`, `""`)+`" WHERE owner_id=?`, identity.OwnerID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(r.Context(), `DELETE FROM owners WHERE id=?`, identity.OwnerID)
		return err
	})
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.revoked", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": prefix, "scope": "owner", "tokens": len(records), "resources": resources, "source": "web"})
	s.setOwnerCookie(w, r, "", true)
	w.Header().Set("Clear-Site-Data", `"storage"`)
	http.Redirect(w, r, tokenPagePath+"?notice=revoked", http.StatusSeeOther)
}

func ownerResourceTables(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('owners','api_tokens')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		columns, err := tx.Query(`PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
		if err != nil {
			return nil, err
		}
		found := false
		for columns.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def sql.NullString
			if err := columns.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				_ = columns.Close()
				return nil, err
			}
			if name == "owner_id" {
				found = true
			}
		}
		if err := columns.Close(); err != nil {
			return nil, err
		}
		if found {
			tables = append(tables, table)
		}
	}
	return tables, rows.Err()
}

func countOwnerResources(ctx context.Context, db *sql.DB, ownerID string) (int64, error) {
	if db == nil {
		return 0, nil
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	tables, err := ownerResourceTables(tx)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, table := range tables {
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`" WHERE owner_id=?`, ownerID).Scan(&n); err != nil {
			return 0, err
		}
		count += n
	}
	return count, nil
}

func (s *Server) handleWebCreateLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, _, ok := s.requireOwnerCookie(w, r, false)
	if !ok {
		return
	}
	name, valid := validLinkedTokenName(r.PostForm.Get("name"))
	if !valid {
		http.Redirect(w, r, tokenPagePath+"?error=validation", http.StatusSeeOther)
		return
	}
	var expires, expiresOn *string
	if value := r.PostForm.Get("expires"); value != "" && value != "custom" {
		expires = &value
	}
	if value := r.PostForm.Get("expires_on"); value != "" {
		expiresOn = &value
	}
	if r.PostForm.Get("expires") == "custom" && expiresOn == nil {
		http.Redirect(w, r, tokenPagePath+"?error=validation", http.StatusSeeOther)
		return
	}
	expiry, err := linkedTokenExpiry(expires, expiresOn, time.Now())
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=validation", http.StatusSeeOther)
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	encrypted, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	max := s.cfg.Server.Tokens.MaxLinked
	if max < 1 {
		max = 25
	}
	record, err := s.db.CreateLinkedToken(r.Context(), identity.OwnerID, name, hashOwnerToken(token), token[:12], token[len(token)-4:], encrypted, expiry, max)
	if errors.Is(err, database.ErrLinkedTokenConflict) {
		http.Redirect(w, r, tokenPagePath+"?error=conflict", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.created", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "linked"})
	s.setTokenViewCookie(w, r, record.ID)
	http.Redirect(w, r, tokenPagePath+"?notice=created#token-"+record.ID, http.StatusSeeOther)
}

func (s *Server) linkedPageAction(w http.ResponseWriter, r *http.Request, limited bool) (database.OwnerTokenIdentity, database.OwnerTokenRecord, bool) {
	identity, _, ok := s.requireOwnerCookie(w, r, limited)
	if !ok {
		return database.OwnerTokenIdentity{}, database.OwnerTokenRecord{}, false
	}
	record, err := s.db.GetLinkedToken(r.Context(), identity.OwnerID, chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		http.Redirect(w, r, tokenPagePath+"?error=invalid", http.StatusSeeOther)
		return database.OwnerTokenIdentity{}, database.OwnerTokenRecord{}, false
	}
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return database.OwnerTokenIdentity{}, database.OwnerTokenRecord{}, false
	}
	return identity, record, true
}

func (s *Server) handleWebRevealLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, record, ok := s.linkedPageAction(w, r, true)
	if !ok {
		return
	}
	if linkedTokenStatus(record, time.Now()) != "active" {
		http.Redirect(w, r, tokenPagePath+"?error=invalid", http.StatusSeeOther)
		return
	}
	if _, err := s.db.LinkedTokenCiphertext(r.Context(), identity.OwnerID, record.ID); err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.revealed", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": record.TokenPrefix, "source": "web"})
	s.setTokenViewCookie(w, r, record.ID)
	http.Redirect(w, r, tokenPagePath+"#token-"+record.ID, http.StatusSeeOther)
}

func (s *Server) handleWebRotateLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, record, ok := s.linkedPageAction(w, r, true)
	if !ok {
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	enc, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	updated, err := s.db.RotateLinkedToken(r.Context(), identity.OwnerID, record.ID, hashOwnerToken(token), token[:12], token[len(token)-4:], enc)
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.rotated", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "web"})
	s.setTokenViewCookie(w, r, record.ID)
	http.Redirect(w, r, tokenPagePath+"?notice=rotated#token-"+updated.ID, http.StatusSeeOther)
}

func (s *Server) handleWebUpdateLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, record, ok := s.linkedPageAction(w, r, false)
	if !ok {
		return
	}
	name, valid := validLinkedTokenName(r.PostForm.Get("name"))
	if !valid {
		http.Redirect(w, r, tokenPagePath+"?error=validation#token-"+record.ID, http.StatusSeeOther)
		return
	}
	var expiry sql.NullInt64
	var err error
	selection := r.PostForm.Get("expires")
	if selection == "unchanged" {
		expiry = record.ExpiresAt
	} else {
		var expires, expiresOn *string
		if selection != "" && selection != "custom" {
			expires = &selection
		}
		if v := r.PostForm.Get("expires_on"); v != "" {
			expiresOn = &v
		}
		if selection == "custom" && expiresOn == nil {
			http.Redirect(w, r, tokenPagePath+"?error=validation#token-"+record.ID, http.StatusSeeOther)
			return
		}
		expiry, err = linkedTokenExpiry(expires, expiresOn, time.Now())
	}
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=validation#token-"+record.ID, http.StatusSeeOther)
		return
	}
	_, err = s.db.UpdateLinkedToken(r.Context(), identity.OwnerID, record.ID, name, expiry)
	if errors.Is(err, database.ErrLinkedTokenConflict) {
		http.Redirect(w, r, tokenPagePath+"?error=conflict#token-"+record.ID, http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, tokenPagePath+"?notice=updated#token-"+record.ID, http.StatusSeeOther)
}

func (s *Server) handleWebRevokeLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, record, ok := s.linkedPageAction(w, r, false)
	if !ok {
		return
	}
	if err := s.db.RevokeLinkedToken(r.Context(), identity.OwnerID, record.ID); err != nil {
		http.Redirect(w, r, tokenPagePath+"?error=unavailable", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, "token.revoked", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": record.TokenPrefix, "source": "web"})
	http.Redirect(w, r, tokenPagePath+"?notice=revoked#token-"+record.ID, http.StatusSeeOther)
}
