package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/database"
)

const maxPGPKeyserverAttempts = 8

var pgpKeyserverPublishMu sync.Mutex

type pgpKeyserverState struct {
	Version int                               `json:"version"`
	Entries map[string]pgpKeyserverStateEntry `json:"entries"`
}

type pgpKeyserverStateEntry struct {
	Fingerprint string     `json:"fingerprint"`
	Attempts    int        `json:"attempts"`
	NextAttempt *time.Time `json:"next_attempt,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

// PublishPGPKeyservers publishes the active public key and persists retry state.
func PublishPGPKeyservers(ctx context.Context, db *database.DB, securityDir string, hosts []string, now time.Time, retryOnly bool) error {
	return publishPGPKeyservers(ctx, db, securityDir, hosts, now, retryOnly, security.PublishPGPKey)
}

func publishPGPKeyservers(ctx context.Context, db *database.DB, securityDir string, hosts []string, now time.Time, retryOnly bool, submit func(context.Context, string, []byte, string) error) error {
	if db == nil {
		return errors.New("server database is unavailable")
	}
	if submit == nil {
		return errors.New("keyserver publisher is unavailable")
	}
	pgpKeyserverPublishMu.Lock()
	defer pgpKeyserverPublishMu.Unlock()

	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil {
		return fmt.Errorf("read PGP key metadata: %w", err)
	}
	if metadata.Revoked || !metadata.ExpiresAt.After(now) {
		return errors.New("active PGP key is revoked or expired")
	}
	publicPath := filepath.Join(securityDir, "pgp.pub.asc")
	publicKey, err := os.ReadFile(publicPath)
	if err != nil {
		return fmt.Errorf("read public PGP key: %w", err)
	}
	fingerprint, err := security.PGPFingerprint(publicKey)
	if err != nil || fingerprint != metadata.Fingerprint {
		return errors.New("public PGP key does not match active metadata")
	}
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		return fmt.Errorf("create PGP key directory: %w", err)
	}
	if err := os.Chmod(securityDir, 0o700); err != nil {
		return fmt.Errorf("secure PGP key directory: %w", err)
	}
	statePath := filepath.Join(securityDir, "keyservers.state")
	state, err := loadPGPKeyserverState(statePath)
	if err != nil {
		return err
	}
	if state.Entries == nil {
		state.Entries = make(map[string]pgpKeyserverStateEntry)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	var failures []error
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" || pgpKeyPublished(metadata.KeyserversPublished, host) {
			continue
		}
		entry := state.Entries[host]
		if entry.Fingerprint != metadata.Fingerprint {
			entry = pgpKeyserverStateEntry{Fingerprint: metadata.Fingerprint}
		}
		if entry.PublishedAt != nil {
			metadata.KeyserversPublished = append(metadata.KeyserversPublished, database.PGPKeyserverPublication{Host: host, PublishedAt: *entry.PublishedAt})
			if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
				failures = append(failures, fmt.Errorf("record keyserver %s publication: %w", host, err))
			}
			continue
		}
		if retryOnly && (entry.NextAttempt == nil || entry.NextAttempt.After(now)) {
			continue
		}
		if entry.Attempts >= maxPGPKeyserverAttempts {
			continue
		}
		if err := submit(ctx, host, publicKey, metadata.Fingerprint); err != nil {
			var keyserverErr *security.PGPKeyserverError
			if errors.As(err, &keyserverErr) && keyserverErr.Retryable {
				entry.Attempts++
				if entry.Attempts < maxPGPKeyserverAttempts {
					delay := time.Minute * time.Duration(1<<min(entry.Attempts-1, 8))
					if delay > 6*time.Hour {
						delay = 6 * time.Hour
					}
					next := now.Add(delay)
					entry.NextAttempt = &next
				} else {
					entry.NextAttempt = nil
				}
			} else {
				entry.Attempts = maxPGPKeyserverAttempts
				entry.NextAttempt = nil
			}
			entry.LastError = err.Error()
			state.Entries[host] = entry
			if saveErr := savePGPKeyserverState(statePath, state); saveErr != nil {
				failures = append(failures, fmt.Errorf("save keyserver retry state: %w", saveErr))
			}
			failures = append(failures, fmt.Errorf("publish to %s: %w", host, err))
			continue
		}
		publishedAt := now
		entry.PublishedAt = &publishedAt
		entry.NextAttempt = nil
		entry.LastError = ""
		state.Entries[host] = entry
		metadata.KeyserversPublished = append(metadata.KeyserversPublished, database.PGPKeyserverPublication{Host: host, PublishedAt: publishedAt})
		if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
			failures = append(failures, fmt.Errorf("record keyserver %s publication: %w", host, err))
		}
		if err := savePGPKeyserverState(statePath, state); err != nil {
			failures = append(failures, fmt.Errorf("save keyserver state: %w", err))
		}
	}
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		if err := savePGPKeyserverState(statePath, state); err != nil {
			failures = append(failures, fmt.Errorf("initialize keyserver state: %w", err))
		}
	}
	return errors.Join(failures...)
}

func pgpKeyPublished(publications []database.PGPKeyserverPublication, host string) bool {
	for _, publication := range publications {
		if strings.EqualFold(publication.Host, host) {
			return true
		}
	}
	return false
}

func loadPGPKeyserverState(path string) (pgpKeyserverState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return pgpKeyserverState{Version: 1, Entries: make(map[string]pgpKeyserverStateEntry)}, nil
	}
	if err != nil {
		return pgpKeyserverState{}, fmt.Errorf("read keyserver state: %w", err)
	}
	var state pgpKeyserverState
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 1 {
		return pgpKeyserverState{}, errors.New("invalid PGP keyserver state file")
	}
	if state.Entries == nil {
		state.Entries = make(map[string]pgpKeyserverStateEntry)
	}
	return state, nil
}

func savePGPKeyserverState(path string, state pgpKeyserverState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode keyserver state: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".keyservers-state-*")
	if err != nil {
		return fmt.Errorf("create temporary keyserver state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace keyserver state: %w", err)
	}
	return nil
}
