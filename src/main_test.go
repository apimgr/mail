package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/paths"
	"github.com/apimgr/mail/src/service"
)

func TestBackupDirectoryFailureReportsPathOwnerModeAndUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup")
	if err := os.WriteFile(path, []byte("backup path is a file"), 0o640); err != nil {
		t.Fatal(err)
	}
	details := backupDirectoryFailure(path, errors.New("not a directory"))
	for _, want := range []string{path, "owner=", "mode=0640", "service_user=", "not a directory"} {
		if !strings.Contains(details, want) {
			t.Errorf("backup diagnostic %q does not contain %q", details, want)
		}
	}
}

func TestSecurityMaintenanceRejectsUnknownActionAndStatus(t *testing.T) {
	var actionCode, statusCode, trackingIDCode, emptyCode, exportCode, updateCode int
	captureStdout(t, func() {
		actionCode = handleSecurityMaintenance([]string{"invalid"})
		statusCode = handleSecurityMaintenance([]string{"list", "--status", "unknown"})
		trackingIDCode = handleSecurityMaintenance([]string{"show", "invalid"})
		emptyCode = handleSecurityMaintenance(nil)
		exportCode = handleSecurityMaintenance([]string{"export", "bad"})
		updateCode = handleSecurityMaintenance([]string{"update", "bad"})
	})
	if got := actionCode; got != 2 {
		t.Fatalf("unknown security maintenance action exit code = %d, want 2", got)
	}
	if got := statusCode; got != 2 {
		t.Fatalf("invalid report status exit code = %d, want 2", got)
	}
	if got := trackingIDCode; got != 2 {
		t.Fatalf("invalid tracking ID exit code = %d, want 2", got)
	}
	if got := emptyCode; got != 2 {
		t.Fatalf("empty security maintenance exit code = %d, want 2", got)
	}
	if got := exportCode; got != 2 {
		t.Fatalf("invalid export tracking ID exit code = %d, want 2", got)
	}
	if got := updateCode; got != 2 {
		t.Fatalf("invalid update tracking ID exit code = %d, want 2", got)
	}
}

func TestSecurityReportMaintenanceHelpAndArgumentValidation(t *testing.T) {
	const trackingID = "sec_0123456789abcdef"
	var exportHelp, updateHelp, exportFlagError, exportExtra, updateNoFields int
	captureStdout(t, func() {
		exportHelp = handleSecurityReportExport([]string{trackingID, "--help"})
		updateHelp = handleSecurityReportUpdate([]string{trackingID, "--help"})
		exportFlagError = handleSecurityReportExport([]string{trackingID, "--unknown"})
		exportExtra = handleSecurityReportExport([]string{trackingID, "extra"})
		updateNoFields = handleSecurityReportUpdate([]string{trackingID})
	})
	if exportHelp != 0 || updateHelp != 0 {
		t.Fatalf("maintenance help codes: export=%d update=%d, want 0", exportHelp, updateHelp)
	}
	if exportFlagError != 2 || exportExtra != 2 || updateNoFields != 2 {
		t.Fatalf("maintenance validation codes: flag=%d extra=%d no-fields=%d, want 2", exportFlagError, exportExtra, updateNoFields)
	}
}

func TestPGPMaintenanceHelpAndArgumentValidation(t *testing.T) {
	var help, missing, invalidExport, unsupportedPrivateExport, extraGenerate, extraRotate, missingImportFile, extraImportArgument, unknown int
	var exportHelp, publicExportExtra, privateExportExtra int
	captureStdout(t, func() {
		help = handlePGPMaintenance([]string{"--help"})
		missing = handlePGPMaintenance(nil)
		invalidExport = handlePGPMaintenance([]string{"export"})
		unsupportedPrivateExport = handlePGPMaintenance([]string{"export", "private"})
		extraGenerate = handlePGPMaintenance([]string{"generate", "extra"})
		extraRotate = handlePGPMaintenance([]string{"rotate", "extra"})
		missingImportFile = handlePGPMaintenance([]string{"import"})
		extraImportArgument = handlePGPMaintenance([]string{"import", "key.asc", "extra"})
		unknown = handlePGPMaintenance([]string{"unknown"})
		exportHelp = handlePGPExport([]string{"--help"})
		publicExportExtra = handlePGPExport([]string{"public", "key.asc", "extra"})
		privateExportExtra = handlePGPPrivateExport([]string{"key.asc", "extra"})
	})
	if help != 0 || missing != 2 || invalidExport != 2 || unsupportedPrivateExport != 2 || extraGenerate != 2 || extraRotate != 2 || missingImportFile != 2 || extraImportArgument != 2 || unknown != 2 || exportHelp != 0 || publicExportExtra != 2 || privateExportExtra != 2 {
		t.Fatalf("PGP maintenance codes: help=%d missing=%d export=%d private_export=%d extra_generate=%d extra_rotate=%d missing_import=%d extra_import=%d unknown=%d export_help=%d public_extra=%d private_extra=%d", help, missing, invalidExport, unsupportedPrivateExport, extraGenerate, extraRotate, missingImportFile, extraImportArgument, unknown, exportHelp, publicExportExtra, privateExportExtra)
	}
}

func TestConfiguredSecurityEmail(t *testing.T) {
	validConfig := &config.Config{}
	validConfig.Server.FQDN = "mail.example.test."
	validConfig.Server.Contact.Security.Email = "security@{fqdn}"
	unresolvedConfig := &config.Config{}
	unresolvedConfig.Server.Contact.Security.Email = "security@{fqdn}"
	invalidConfig := &config.Config{}
	invalidConfig.Server.Contact.Security.Email = "Security Team <security@example.test>"
	tests := []struct {
		name  string
		cfg   *config.Config
		want  string
		valid bool
	}{
		{name: "nil config"},
		{name: "missing email", cfg: &config.Config{}},
		{name: "unresolved host", cfg: unresolvedConfig},
		{name: "resolved host", cfg: validConfig, want: "security@mail.example.test", valid: true},
		{name: "invalid address", cfg: invalidConfig},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := configuredSecurityEmail(test.cfg)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("configuredSecurityEmail() = %q, %v; want %q", got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("configuredSecurityEmail() = %q, nil error; want error", got)
			}
		})
	}
}

func TestPGPIdentityMatches(t *testing.T) {
	tests := []struct {
		name       string
		identities []string
		email      string
		want       bool
	}{
		{name: "matching identity", identities: []string{"Mail Security <security@example.test>"}, email: "security@example.test", want: true},
		{name: "case insensitive email", identities: []string{"Mail Security <Security@Example.Test>"}, email: "security@example.test", want: true},
		{name: "wrong name", identities: []string{"Other <security@example.test>"}, email: "security@example.test"},
		{name: "wrong email", identities: []string{"Mail Security <other@example.test>"}, email: "security@example.test"},
		{name: "malformed identity", identities: []string{"Mail Security security@example.test"}, email: "security@example.test"},
		{name: "empty identities", email: "security@example.test"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pgpIdentityMatches(test.identities, test.email); got != test.want {
				t.Errorf("pgpIdentityMatches(%q, %q) = %t, want %t", test.identities, test.email, got, test.want)
			}
		})
	}
}

func TestWriteExclusiveFileRejectsMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "key.asc")
	if err := writeExclusiveFile(path, []byte("private material")); err == nil {
		t.Fatal("writeExclusiveFile() succeeded without its parent directory")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed write created destination: %v", err)
	}
}

func TestInspectPGPImport(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	_, validPrivate, _, _, err := security.GeneratePGPKey("Mail Security <security@example.test>", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, mismatchedPrivate, _, _, err := security.GeneratePGPKey("Other <other@example.test>", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, expiredPrivate, _, _, err := security.GeneratePGPKey("Mail Security <security@example.test>", now.AddDate(-3, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, futurePrivate, _, _, err := security.GeneratePGPKey("Mail Security <security@example.test>", now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	valid, err := inspectPGPImport(validPrivate, "security@example.test", now)
	if err != nil || valid.fingerprint == "" || len(valid.publicArmor) == 0 || !valid.identityMatches || valid.created.After(now) || !valid.expires.After(now) {
		t.Fatalf("inspect valid import = %+v, %v", valid, err)
	}
	mismatched, err := inspectPGPImport(mismatchedPrivate, "security@example.test", now)
	if err != nil || mismatched.identityMatches {
		t.Fatalf("inspect mismatched identity = %+v, %v; want valid key with mismatch", mismatched, err)
	}
	for _, test := range []struct {
		name string
		key  []byte
	}{
		{name: "malformed key", key: []byte("not a private key")},
		{name: "expired key", key: expiredPrivate},
		{name: "future key", key: futurePrivate},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := inspectPGPImport(test.key, "security@example.test", now); err == nil {
				t.Fatal("inspectPGPImport() error = nil, want rejection")
			}
		})
	}
}

func TestStorePGPImportSecuresFilesAndPersistsMetadata(t *testing.T) {
	db := newPGPImportDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, privateArmor, fingerprint, expires, err := security.GeneratePGPKey("Mail Security <security@example.test>", now)
	if err != nil {
		t.Fatal(err)
	}
	imported := inspectedPGPImport{fingerprint: fingerprint, created: now, expires: expires, publicArmor: []byte("public key armor")}
	securityDir := filepath.Join(t.TempDir(), "config", "security")
	if err := storePGPImport(context.Background(), db, securityDir, privateArmor, imported); err != nil {
		t.Fatalf("storePGPImport() error = %v", err)
	}
	for _, path := range []string{securityDir, filepath.Join(securityDir, "pgp.priv.asc.enc"), filepath.Join(securityDir, "pgp.pub.asc")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		wantMode := os.FileMode(0o700)
		if path != securityDir {
			wantMode = 0o600
		}
		if info.Mode().Perm() != wantMode {
			t.Errorf("%s mode = %04o, want %04o", path, info.Mode().Perm(), wantMode)
		}
	}
	storedPrivate, err := os.ReadFile(filepath.Join(securityDir, "pgp.priv.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	installationSecret, err := db.GeneratedSecret(context.Background(), "installation_secret")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := security.DecryptPGPPrivateKey(storedPrivate, installationSecret)
	if err != nil || !bytes.Equal(decrypted, privateArmor) {
		t.Fatalf("stored private key decrypt = %t, %v", bytes.Equal(decrypted, privateArmor), err)
	}
	storedPublic, err := os.ReadFile(filepath.Join(securityDir, "pgp.pub.asc"))
	if err != nil || !bytes.Equal(storedPublic, imported.publicArmor) {
		t.Fatalf("stored public key = %q, %v", storedPublic, err)
	}
	metadata, err := db.GetPGPKeyMetadata(context.Background())
	if err != nil || metadata.Fingerprint != fingerprint || !metadata.CreatedAt.Equal(now) || !metadata.ExpiresAt.Equal(expires) || metadata.Revoked {
		t.Fatalf("stored PGP metadata = %+v, %v", metadata, err)
	}
}

func TestStorePGPImportRejectsActiveKeysAndExistingFiles(t *testing.T) {
	ctx := context.Background()
	imported := inspectedPGPImport{fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567", created: time.Now().UTC(), expires: time.Now().UTC().Add(time.Hour), publicArmor: []byte("public")}
	activeDB := newPGPImportDatabase(t)
	if err := activeDB.SavePGPKeyMetadata(ctx, database.PGPKeyMetadata{Fingerprint: imported.fingerprint, CreatedAt: imported.created, ExpiresAt: imported.expires}); err != nil {
		t.Fatal(err)
	}
	if err := storePGPImport(ctx, activeDB, filepath.Join(t.TempDir(), "security"), []byte("private"), imported); err == nil || !strings.Contains(err.Error(), "active PGP keypair") {
		t.Fatalf("active-key import error = %v", err)
	}

	filesDB := newPGPImportDatabase(t)
	securityDir := t.TempDir()
	publicPath := filepath.Join(securityDir, "pgp.pub.asc")
	if err := os.WriteFile(publicPath, []byte("existing public key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storePGPImport(ctx, filesDB, securityDir, []byte("private"), imported); err == nil || !strings.Contains(err.Error(), "already exist") {
		t.Fatalf("existing-file import error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(securityDir, "pgp.priv.asc.enc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private key was created despite existing public key: %v", err)
	}
	if err := storePGPImport(ctx, nil, securityDir, []byte("private"), imported); err == nil {
		t.Fatal("storePGPImport accepted a nil database")
	}

	revokedDB := newPGPImportDatabase(t)
	if err := revokedDB.SavePGPKeyMetadata(ctx, database.PGPKeyMetadata{Fingerprint: imported.fingerprint, CreatedAt: imported.created, ExpiresAt: imported.expires, Revoked: true}); err != nil {
		t.Fatal(err)
	}
	if err := storePGPImport(ctx, revokedDB, filepath.Join(t.TempDir(), "security"), []byte("private"), imported); err != nil {
		t.Fatalf("import after revocation error = %v", err)
	}

	failedDB := newPGPImportDatabase(t)
	if err := failedDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := storePGPImport(ctx, failedDB, filepath.Join(t.TempDir(), "security"), []byte("private"), imported); err == nil || !strings.Contains(err.Error(), "read PGP key metadata") {
		t.Fatalf("database read failure = %v", err)
	}

	directoryDB := newPGPImportDatabase(t)
	blockedDirectory := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedDirectory, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storePGPImport(ctx, directoryDB, blockedDirectory, []byte("private"), imported); err == nil || !strings.Contains(err.Error(), "create PGP key directory") {
		t.Fatalf("key-directory creation failure = %v", err)
	}
}

func TestRotatePGPKeyReencryptsReportsAndRetainsPreviousPrivateKey(t *testing.T) {
	db, configDir, _, oldFingerprint := newPGPExportFixture(t)
	ctx := context.Background()
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		t.Fatal(err)
	}
	securityDir := filepath.Join(configDir, "security")
	oldPublic, err := os.ReadFile(filepath.Join(securityDir, "pgp.pub.asc"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("report content that must survive rotation")
	encryptedBody, err := security.EncryptPGPMessage(oldPublic, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Second)
	if err := db.CreateSecurityReport(ctx, database.SecurityReport{
		TrackingID: "sec_0123456789abcdef", IDWindow: 1, Severity: "high", Component: "mail",
		BodyEncrypted: encryptedBody, BodyAlgorithm: "pgp", PGPFingerprint: oldFingerprint,
		ContactEncrypted: []byte("contact"), TokenHash: strings.Repeat("a", 64), CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}

	firstRotationAt := time.Now().UTC().Truncate(time.Second)
	first, err := rotatePGPKey(ctx, db, securityDir, installationSecret, "Mail Security <security@example.test>", firstRotationAt)
	if err != nil {
		t.Fatalf("rotatePGPKey() error = %v", err)
	}
	if first.Fingerprint == oldFingerprint || first.LastRotatedAt == nil || !first.LastRotatedAt.Equal(firstRotationAt) {
		t.Fatalf("first rotation metadata = %+v", first)
	}
	newPrivateCiphertext, err := os.ReadFile(filepath.Join(securityDir, "pgp.priv.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	newPrivate, err := security.DecryptPGPPrivateKey(newPrivateCiphertext, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := security.PGPPrivateFingerprint(newPrivate); err != nil || got != first.Fingerprint {
		t.Fatalf("active rotated private fingerprint = %q, %v; want %q", got, err, first.Fingerprint)
	}
	retiredCiphertext, err := os.ReadFile(filepath.Join(securityDir, "pgp.priv.old.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	retiredPrivate, err := security.DecryptPGPPrivateKey(retiredCiphertext, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := security.PGPPrivateFingerprint(retiredPrivate); err != nil || got != oldFingerprint {
		t.Fatalf("retired private fingerprint = %q, %v; want %q", got, err, oldFingerprint)
	}
	rotatedReport, err := db.GetEncryptedSecurityReport(ctx, "sec_0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	decryptedReport, err := security.DecryptPGPMessage(newPrivate, rotatedReport.Body)
	if err != nil || !bytes.Equal(decryptedReport, plaintext) {
		t.Fatalf("rotated report decrypt = %q, %v", decryptedReport, err)
	}
	for _, name := range []string{"pgp.priv.asc.enc.new", "pgp.pub.asc.new"} {
		if fileExists(filepath.Join(securityDir, name)) {
			t.Errorf("rotation left staged key file %s", name)
		}
	}

	secondRotationAt := time.Now().UTC().Truncate(time.Second)
	second, err := rotatePGPKey(ctx, db, securityDir, installationSecret, "Mail Security <security@example.test>", secondRotationAt)
	if err != nil {
		t.Fatalf("second rotatePGPKey() error = %v", err)
	}
	retiredCiphertext, err = os.ReadFile(filepath.Join(securityDir, "pgp.priv.old.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	retiredPrivate, err = security.DecryptPGPPrivateKey(retiredCiphertext, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := security.PGPPrivateFingerprint(retiredPrivate); err != nil || got != first.Fingerprint {
		t.Fatalf("second rotation retained fingerprint = %q, %v; want previous active %q", got, err, first.Fingerprint)
	}
	if second.Fingerprint == first.Fingerprint {
		t.Fatal("second rotation reused the active fingerprint")
	}
}

func TestRotatePGPKeyRollsBackOnUnreadableReport(t *testing.T) {
	db, configDir, _, oldFingerprint := newPGPExportFixture(t)
	ctx := context.Background()
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Second)
	if err := db.CreateSecurityReport(ctx, database.SecurityReport{
		TrackingID: "sec_0123456789abcdef", IDWindow: 1, Severity: "high", Component: "mail",
		BodyEncrypted: []byte("broken PGP ciphertext"), BodyAlgorithm: "pgp", PGPFingerprint: oldFingerprint,
		ContactEncrypted: []byte("contact"), TokenHash: strings.Repeat("a", 64), CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rotatePGPKey(ctx, db, filepath.Join(configDir, "security"), installationSecret, "Mail Security <security@example.test>", created.Add(time.Minute)); err == nil {
		t.Fatal("rotatePGPKey() succeeded with unreadable retained report")
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil || metadata.Fingerprint != oldFingerprint {
		t.Fatalf("failed rotation changed metadata: %+v, %v", metadata, err)
	}
	for _, name := range []string{"pgp.priv.asc.enc.new", "pgp.pub.asc.new"} {
		if fileExists(filepath.Join(configDir, "security", name)) {
			t.Errorf("failed rotation left staged key file %s", name)
		}
	}
}

func TestRecoverPGPRotationDiscardsUncommittedStagesAndCompletesCommittedStages(t *testing.T) {
	db, configDir, oldPrivate, oldFingerprint := newPGPExportFixture(t)
	ctx := context.Background()
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		t.Fatal(err)
	}
	securityDir := filepath.Join(configDir, "security")
	created := time.Now().UTC().Truncate(time.Second)
	newPublic, newPrivate, newFingerprint, expires, err := security.GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	signedPublic, err := security.SignPGPPublicKey(newPublic, oldPrivate)
	if err != nil {
		t.Fatal(err)
	}
	encryptedNewPrivate, err := security.EncryptPGPPrivateKey(newPrivate, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	writeStages := func() {
		t.Helper()
		if err := writeExclusiveFile(filepath.Join(securityDir, "pgp.priv.asc.enc.new"), encryptedNewPrivate); err != nil {
			t.Fatal(err)
		}
		if err := writeExclusiveFile(filepath.Join(securityDir, "pgp.pub.asc.new"), signedPublic); err != nil {
			t.Fatal(err)
		}
	}
	writeStages()
	if err := recoverPGPRotationFiles(ctx, db, securityDir, installationSecret); err != nil {
		t.Fatalf("recover uncommitted stages: %v", err)
	}
	if fileExists(filepath.Join(securityDir, "pgp.priv.asc.enc.new")) || fileExists(filepath.Join(securityDir, "pgp.pub.asc.new")) {
		t.Fatal("uncommitted PGP rotation stages were not discarded")
	}
	if fileExists(filepath.Join(securityDir, "pgp.priv.old.asc.enc")) {
		t.Fatal("uncommitted rotation created a retired key")
	}

	rotatedAt := time.Now().UTC().Truncate(time.Second)
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metadata.Fingerprint = newFingerprint
	metadata.CreatedAt = created
	metadata.ExpiresAt = expires
	metadata.LastRotatedAt = &rotatedAt
	metadata.KeyserversPublished = []database.PGPKeyserverPublication{}
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	stagedPrivatePath := filepath.Join(securityDir, "pgp.priv.asc.enc.new")
	stagedPublicPath := filepath.Join(securityDir, "pgp.pub.asc.new")
	if err := writeExclusiveFile(stagedPrivatePath, encryptedNewPrivate); err != nil {
		t.Fatal(err)
	}
	oldPublic, err := os.ReadFile(filepath.Join(securityDir, "pgp.pub.asc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeExclusiveFile(stagedPublicPath, oldPublic); err != nil {
		t.Fatal(err)
	}
	if err := recoverPGPRotationFiles(ctx, db, securityDir, installationSecret); err == nil {
		t.Fatal("recovery accepted a staged public key that conflicts with committed metadata")
	}
	if !fileExists(stagedPrivatePath) || !fileExists(stagedPublicPath) {
		t.Fatal("recovery discarded stages needed to repair a committed rotation")
	}
	if err := os.Remove(stagedPrivatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stagedPublicPath); err != nil {
		t.Fatal(err)
	}
	writeStages()
	if err := recoverPGPRotationFiles(ctx, db, securityDir, installationSecret); err != nil {
		t.Fatalf("recover committed stages: %v", err)
	}
	activeCiphertext, err := os.ReadFile(filepath.Join(securityDir, "pgp.priv.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	activePrivate, err := security.DecryptPGPPrivateKey(activeCiphertext, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := security.PGPPrivateFingerprint(activePrivate); err != nil || got != newFingerprint {
		t.Fatalf("recovered active fingerprint = %q, %v; want %q", got, err, newFingerprint)
	}
	retiredCiphertext, err := os.ReadFile(filepath.Join(securityDir, "pgp.priv.old.asc.enc"))
	if err != nil {
		t.Fatal(err)
	}
	retiredPrivate, err := security.DecryptPGPPrivateKey(retiredCiphertext, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := security.PGPPrivateFingerprint(retiredPrivate); err != nil || got != oldFingerprint {
		t.Fatalf("recovered retired fingerprint = %q, %v; want %q", got, err, oldFingerprint)
	}
}

func newPGPImportDatabase(t *testing.T) *database.DB {
	t.Helper()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestReadPGPSensitiveConfirmation(t *testing.T) {
	for _, test := range []struct {
		name          string
		input         string
		requireReason bool
		wantReason    string
		wantOK        bool
	}{
		{name: "reason", input: "EXPORT PRIVATE PGP KEY\nIncident response\n", requireReason: true, wantReason: "Incident response", wantOK: true},
		{name: "wrong phrase", input: "WRONG\n", wantOK: false},
		{name: "missing reason", input: "EXPORT PRIVATE PGP KEY\n", requireReason: true, wantOK: false},
		{name: "delete phrase", input: "DELETE PGP KEY\n", wantOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			reason, ok := readPGPSensitiveConfirmation(bufio.NewReader(strings.NewReader(test.input)), &output, map[bool]string{true: "EXPORT PRIVATE PGP KEY", false: "DELETE PGP KEY"}[test.requireReason], test.requireReason)
			if ok != test.wantOK || reason != test.wantReason {
				t.Fatalf("readPGPSensitiveConfirmation() = %q, %t; want %q, %t", reason, ok, test.wantReason, test.wantOK)
			}
			if output.Len() == 0 {
				t.Fatal("confirmation prompt was not written")
			}
		})
	}
}

func TestDeletePGPKeyFilesRevokesMetadataAndRemovesKeyMaterial(t *testing.T) {
	db, securityDir, metadata := newPGPDeleteFixture(t)
	if err := deletePGPKeyFiles(context.Background(), db, securityDir, metadata); err != nil {
		t.Fatalf("deletePGPKeyFiles() error = %v", err)
	}
	for _, name := range []string{"pgp.priv.asc.enc", "pgp.pub.asc", "pgp.priv.old.asc.enc", "pgp.priv.asc.enc.new", "pgp.pub.asc.new", "keyservers.state"} {
		if _, err := os.Lstat(filepath.Join(securityDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("deleted file %s still exists or returned error: %v", name, err)
		}
	}
	got, err := db.GetPGPKeyMetadata(context.Background())
	if err != nil || !got.Revoked || got.Fingerprint != metadata.Fingerprint {
		t.Fatalf("revoked metadata = %#v, error %v", got, err)
	}
}

func TestDeletePGPKeyFilesRestoresFilesWhenMetadataWriteFails(t *testing.T) {
	db, securityDir, metadata := newPGPDeleteFixture(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := deletePGPKeyFiles(context.Background(), db, securityDir, metadata); err == nil {
		t.Fatal("deletePGPKeyFiles() error = nil, want database write failure")
	}
	for _, name := range []string{"pgp.priv.asc.enc", "pgp.pub.asc", "pgp.priv.old.asc.enc", "pgp.priv.asc.enc.new", "pgp.pub.asc.new"} {
		if _, err := os.Stat(filepath.Join(securityDir, name)); err != nil {
			t.Errorf("rollback did not restore %s: %v", name, err)
		}
	}
}

func TestExportPGPPrivateKeyAuditsAndEnforcesRateLimit(t *testing.T) {
	db, configDir, privateArmor, fingerprint := newPGPExportFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	outputPath := filepath.Join(t.TempDir(), "private.asc")
	if err := exportPGPPrivateKey(context.Background(), db, configDir, outputPath, "operator-test", "incident response", now); err != nil {
		t.Fatalf("exportPGPPrivateKey() error = %v", err)
	}
	exported, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(exported, privateArmor) {
		t.Fatalf("exported private key matches generated key = %t, error %v", bytes.Equal(exported, privateArmor), err)
	}
	info, err := os.Stat(outputPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private export permissions = %v, error %v", info, err)
	}
	var auditCount int
	if err := db.ServerReadDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE category = 'security' AND action = 'private_key_exported' AND actor_id = ? AND actor_ip = 'local' AND target_id = ?`, "operator-test", fingerprint).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("private export audit count = %d, error %v; want attempt and success events", auditCount, err)
	}
	if err := exportPGPPrivateKey(context.Background(), db, configDir, filepath.Join(t.TempDir(), "second.asc"), "operator-test", "second attempt", now.Add(30*time.Minute)); err == nil || !strings.Contains(err.Error(), "one attempt per hour") {
		t.Fatalf("second private export error = %v, want rate-limit rejection", err)
	}
}

func TestExportPGPPrivateKeyRejectsUnavailableOrMismatchedKeys(t *testing.T) {
	now := time.Now().UTC()
	if err := exportPGPPrivateKey(context.Background(), nil, "", "key.asc", "operator", "reason", now); err == nil {
		t.Fatal("export with missing dependencies succeeded")
	}
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, db *database.DB, configDir string)
	}{
		{name: "missing private file", setup: func(t *testing.T, _ *database.DB, configDir string) {
			if err := os.Remove(filepath.Join(configDir, "security", "pgp.priv.asc.enc")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "invalid ciphertext", setup: func(t *testing.T, _ *database.DB, configDir string) {
			if err := os.WriteFile(filepath.Join(configDir, "security", "pgp.priv.asc.enc"), []byte("invalid"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "expired key", setup: func(t *testing.T, db *database.DB, _ string) {
			metadata, err := db.GetPGPKeyMetadata(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			metadata.ExpiresAt = time.Now().UTC().Add(-time.Minute)
			metadata.CreatedAt = metadata.ExpiresAt.AddDate(-2, 0, 0)
			if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "revoked key", setup: func(t *testing.T, db *database.DB, _ string) {
			metadata, err := db.GetPGPKeyMetadata(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			metadata.Revoked = true
			if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "private key fingerprint mismatch", setup: func(t *testing.T, db *database.DB, configDir string) {
			secret, err := db.GeneratedSecret(context.Background(), "installation_secret")
			if err != nil {
				t.Fatal(err)
			}
			_, otherPrivate, _, _, err := security.GeneratePGPKey("Other Security <other@example.test>", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			ciphertext, err := security.EncryptPGPPrivateKey(otherPrivate, secret)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDir, "security", "pgp.priv.asc.enc"), ciphertext, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, configDir, _, _ := newPGPExportFixture(t)
			test.setup(t, db, configDir)
			err := exportPGPPrivateKey(context.Background(), db, configDir, filepath.Join(t.TempDir(), "private.asc"), "operator-test", "test reason", now)
			if err == nil {
				t.Fatal("exportPGPPrivateKey() error = nil")
			}
		})
	}
}

func TestExportPGPPrivateKeyDoesNotOverwriteDestination(t *testing.T) {
	db, configDir, _, _ := newPGPExportFixture(t)
	outputPath := filepath.Join(t.TempDir(), "private.asc")
	if err := os.WriteFile(outputPath, []byte("operator data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exportPGPPrivateKey(context.Background(), db, configDir, outputPath, "operator-test", "backup validation", time.Now().UTC()); err == nil {
		t.Fatal("export overwrote an existing destination")
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil || string(contents) != "operator data" {
		t.Fatalf("existing destination = %q, error %v", contents, err)
	}
}

func newPGPExportFixture(t *testing.T) (*database.DB, string, []byte, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Second)
	publicArmor, privateArmor, fingerprint, expires, err := security.GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	encryptedPrivate, err := security.EncryptPGPPrivateKey(privateArmor, installationSecret)
	if err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "config")
	securityDir := filepath.Join(configDir, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, "pgp.priv.asc.enc"), encryptedPrivate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), publicArmor, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := database.PGPKeyMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: expires, KeyserversPublished: []database.PGPKeyserverPublication{}}
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	return db, configDir, privateArmor, fingerprint
}

func newPGPDeleteFixture(t *testing.T) (*database.DB, string, database.PGPKeyMetadata) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	metadata := database.PGPKeyMetadata{Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567", CreatedAt: created, ExpiresAt: created.AddDate(2, 0, 0), KeyserversPublished: []database.PGPKeyserverPublication{}}
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	securityDir := filepath.Join(root, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pgp.priv.asc.enc", "pgp.pub.asc", "pgp.priv.old.asc.enc", "pgp.priv.asc.enc.new", "pgp.pub.asc.new", "keyservers.state"} {
		if err := os.WriteFile(filepath.Join(securityDir, name), []byte("test key data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return db, securityDir, metadata
}

func TestValidSecurityTrackingID(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{"sec_0123456789abcdef", true},
		{"sec_0123456789abcde", false},
		{"bad_0123456789abcdef", false},
		{"sec_0123456789abcdeG", false},
	} {
		if got := validSecurityTrackingID(test.value); got != test.want {
			t.Errorf("validSecurityTrackingID(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

func TestWriteEncryptedSecurityExport(t *testing.T) {
	ciphertext := []byte("v1:encrypted-payload")
	var stdout bytes.Buffer
	if path, err := writeEncryptedSecurityExport("-", ciphertext, &stdout); err != nil || path != "-" || !bytes.Equal(stdout.Bytes(), ciphertext) {
		t.Fatalf("stdout export = path %q, content %q, error %v", path, stdout.Bytes(), err)
	}
	path := filepath.Join(t.TempDir(), "report.asc")
	writtenPath, err := writeEncryptedSecurityExport(path, ciphertext, &stdout)
	if err != nil || writtenPath != path {
		t.Fatalf("file export = path %q, error %v", writtenPath, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %04o, want 0600", info.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, ciphertext) {
		t.Fatalf("export contents = %q, error %v", contents, err)
	}
	if _, err := writeEncryptedSecurityExport(path, []byte("replacement"), &stdout); err == nil {
		t.Fatal("export overwrote an existing file")
	}
	contents, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, ciphertext) {
		t.Fatalf("existing export changed after refused overwrite: %q, error %v", contents, err)
	}
}

func TestUpdateConfigPathHonorsConfigOverride(t *testing.T) {
	previous := cliOptions
	t.Cleanup(func() { cliOptions = previous })
	cliOptions.configDir = filepath.Join(t.TempDir(), "custom-config")
	if got, want := updateConfigPath(), filepath.Join(cliOptions.configDir, "server.yml"); got != want {
		t.Fatalf("update config path = %q, want %q", got, want)
	}
}

func TestBuildEpochAndSetupToken(t *testing.T) {
	previous := BuildEpoch
	defer func() { BuildEpoch = previous }()
	for value, want := range map[string]int64{"0": 0, "123": 123, "invalid": 0} {
		BuildEpoch = value
		if got := buildEpoch(); got != want {
			t.Errorf("buildEpoch(%q) = %d, want %d", value, got, want)
		}
	}
	token, err := generateSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(token) {
		t.Fatalf("setup token has invalid format: %q", token)
	}
}

func TestConfigSetupAndPIDPaths(t *testing.T) {
	root := t.TempDir()
	appPaths := &paths.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), DBDir: filepath.Join(root, "db"), PID: filepath.Join(root, "run", "mail.pid")}
	cfg := createDefaultConfig(appPaths)
	if cfg.Server.Address != "[::]" || cfg.Server.Port < 64000 || cfg.Server.Port > 64999 || cfg.Server.Mode != "production" || cfg.Database.Name != filepath.Join(appPaths.DBDir, "server.db") || cfg.Server.Security.Reports.RetentionDays != 365 {
		t.Fatalf("default config = %#v", cfg)
	}
	cfg.Server.Address = "[::]"
	if appPaths.PID != filepath.Join(root, "run", "mail.pid") {
		t.Fatalf("configured PID path = %q", appPaths.PID)
	}
	if err := os.MkdirAll(appPaths.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(appPaths.Config, "server.yml")
	cfg.Server.SetupToken = "setup-token"
	cfg.Server.Token = "test-token"
	if err := saveConfig(cfg, configFile); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	var loaded *config.Config
	captureStdout(t, func() {
		loaded, err = loadConfig(appPaths)
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Address != cfg.Server.Address || loaded.Server.SetupToken != cfg.Server.SetupToken || loaded.Server.Mode != cfg.Server.Mode || loaded.Database.Name != cfg.Database.Name || !slices.Equal(loaded.Server.I18n.AvailableLanguages, cfg.Server.I18n.AvailableLanguages) {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func TestLoadConfigFirstRunCreatesOperatorSetup(t *testing.T) {
	root := t.TempDir()
	appPaths := &paths.Paths{
		Config: filepath.Join(root, "config"),
		Data:   filepath.Join(root, "data"),
		DBDir:  filepath.Join(root, "data", "db"),
	}
	if err := os.MkdirAll(appPaths.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	var cfg *config.Config
	var err error
	captureStdout(t, func() {
		cfg, err = loadConfig(appPaths)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.SetupToken == "" || cfg.Server.Token == "" {
		t.Fatal("first-run configuration did not generate setup and operator tokens")
	}
	if _, err := os.Stat(filepath.Join(appPaths.Config, "server.yml")); err != nil {
		t.Fatalf("server config was not saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appPaths.Config, "secrets.yml")); err != nil {
		t.Fatalf("operator secrets were not saved: %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousOut, previousErr := os.Stdout, os.Stderr
	os.Stdout = writer
	os.Stderr = writer
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = previousOut, previousErr
	data, err := io.ReadAll(reader)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHelpAndVersionOutput(t *testing.T) {
	for name, fn := range map[string]func(){
		"help":        printHelp,
		"version":     printVersion,
		"shell":       printShellHelp,
		"service":     printServiceHelp,
		"maintenance": printMaintenanceHelp,
		"update":      printUpdateHelp,
	} {
		output := captureStdout(t, fn)
		if output == "" {
			t.Errorf("%s output is empty", name)
		}
	}
	output := captureStdout(t, func() { displayStartupBanner(createDefaultConfig(&paths.Paths{})) })
	if output == "" {
		t.Fatal("startup banner output is empty")
	}
	output = captureStdout(t, func() { displaySetupToken("test-token", &config.Config{}) })
	if output == "" {
		t.Fatal("setup token output is empty")
	}
}

func TestPrintServiceStatus(t *testing.T) {
	output := captureStdout(t, func() {
		printServiceStatus(service.Status{
			Manager:   service.Systemd,
			Scope:     service.System,
			Installed: true,
			Running:   true,
			Enabled:   true,
			PID:       42,
		})
	})
	for _, want := range []string{"installed", "systemd", "running", "enabled", "42"} {
		if !strings.Contains(output, want) {
			t.Errorf("service status output %q does not contain %q", output, want)
		}
	}
}

func TestEmailMaintenancePreviewValidatesAndRendersTemplates(t *testing.T) {
	configDir := t.TempDir()
	output := captureStdout(t, func() {
		if got := previewEmailTemplates(configDir); got != 0 {
			t.Errorf("valid template preview exit = %d", got)
		}
	})
	for _, value := range []string{"[security_report_received]", "[security_alert]", "Subject:"} {
		if !strings.Contains(output, value) {
			t.Errorf("template preview omitted %q", value)
		}
	}
	templateDir := filepath.Join(configDir, "template", "email")
	if err := os.MkdirAll(templateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "security_alert.tmpl"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if got := previewEmailTemplates(configDir); got != 65 {
			t.Errorf("invalid template preview exit = %d, want 65", got)
		}
	})
}

func TestMaintenanceEmailAndModeArgumentValidation(t *testing.T) {
	if got := handleEmailMaintenance(nil); got != 2 {
		t.Errorf("empty email maintenance exit = %d", got)
	}
	if got := handleEmailMaintenance([]string{"test", "unexpected"}); got != 2 {
		t.Errorf("extra email test argument exit = %d", got)
	}
	if got := handleModeMaintenance(nil); got != 2 {
		t.Errorf("empty mode maintenance exit = %d", got)
	}
	if got := handleModeMaintenance([]string{"invalid"}); got != 2 {
		t.Errorf("invalid mode maintenance exit = %d", got)
	}
	if got := handleEmailMaintenance([]string{"test", "--help"}); got != 0 {
		t.Errorf("email maintenance help exit = %d", got)
	}
}

func TestEmailDryRunAndModeChangeUseOnlyConfiguredPaths(t *testing.T) {
	previous := cliOptions
	t.Cleanup(func() { cliOptions = previous })
	root := t.TempDir()
	cliOptions.configDir = filepath.Join(root, "config")
	cliOptions.dataDir = filepath.Join(root, "data")
	cliOptions.cacheDir = filepath.Join(root, "cache")
	cliOptions.logDir = filepath.Join(root, "logs")
	cliOptions.backupDir = filepath.Join(root, "backups")
	cliOptions.pidFile = filepath.Join(root, "run", "mail.pid")
	if err := os.MkdirAll(cliOptions.configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if output := captureStdout(t, func() {
		if got := handleEmailMaintenance([]string{"test", "--dry-run"}); got != 0 {
			t.Errorf("email dry-run exit = %d", got)
		}
	}); !strings.Contains(output, "[security_report_received]") {
		t.Fatal("email dry-run did not include the security-report template")
	}
	output := captureStdout(t, func() {
		if got := handleModeMaintenance([]string{"development"}); got != 0 {
			t.Errorf("mode update exit = %d", got)
		}
	})
	if !strings.Contains(output, "Application mode set to development") {
		t.Fatalf("mode update output = %q", output)
	}
	loaded, err := config.Load(filepath.Join(cliOptions.configDir, "server.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Mode != "development" {
		t.Fatalf("persisted mode = %q", loaded.Server.Mode)
	}
}

func TestLoggerConfigUsesPerKindSettings(t *testing.T) {
	enabled := true
	settings := config.LogsConfig{
		Defaults: config.LogDefaultsConfig{Rotate: "weekly,50MB", Keep: "none"},
		Access:   config.LogKindConfig{Enabled: &enabled, Format: "custom", Custom: "{remote_ip} {status}", Rotate: "daily", Keep: "3", Compress: true},
	}
	got := loggerConfig(settings, "/var/log/mail")
	if got.LogDir != "/var/log/mail" || got.Formats["access"] != "custom" || got.Custom["access"] != "{remote_ip} {status}" || got.Disabled["access"] || got.Policies["access"].Rotate != "daily" || got.Policies["access"].Keep != "3" || !got.Policies["access"].Compress {
		t.Fatalf("logger config did not preserve access settings: %+v", got)
	}
	if !got.Disabled["debug"] || got.Policies["server"].Rotate != "weekly,50MB" || got.Policies["server"].Keep != "none" {
		t.Fatalf("logger defaults were not applied: %+v", got)
	}
}

func TestParseServerArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		check func(serverOptions) bool
	}{
		{name: "server options", args: []string{"--mode=debug", "--port", "8080", "--debug", "--config", "/tmp/mail-config", "--color", "no"}, check: func(o serverOptions) bool {
			return o.mode == "debug" && o.port == 8080 && o.debug && o.configDir == "/tmp/mail-config" && o.color == "no"
		}},
		{name: "service value may start with dash", args: []string{"--service", "--install"}, check: func(o serverOptions) bool { return o.service == "--install" }},
		{name: "shell command and operand", args: []string{"--shell", "completions", "bash"}, check: func(o serverOptions) bool {
			return o.shell == "completions" && len(o.commandArgs) == 1 && o.commandArgs[0] == "bash"
		}},
		{name: "bare update defaults to install", args: []string{"--update"}, check: func(o serverOptions) bool { return o.update == "yes" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseServerArgs(tc.args, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.check(got) {
				t.Fatalf("parsed options = %#v", got)
			}
		})
	}
	for _, args := range [][]string{{"--unknown"}, {"--mode", "invalid"}, {"--port", "70000"}, {"--color", "sometimes"}} {
		if _, err := parseServerArgs(args, io.Discard); err == nil {
			t.Errorf("parseServerArgs(%q) succeeded", args)
		}
	}
	if command, ok := commandHelp([]string{"--service", "--help"}, "service"); !ok || command != "service" {
		t.Fatalf("commandHelp() = %q, %t", command, ok)
	}
}

func TestMaintenanceUpdateAliasesUpdateCommand(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "default install", want: []string{"mail", "--update", "yes"}},
		{name: "check", args: []string{"check"}, want: []string{"mail", "--update", "check"}},
		{name: "branch", args: []string{"branch", "beta"}, want: []string{"mail", "--update", "branch", "beta"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := maintenanceUpdateArgs("mail", test.args); !slices.Equal(got, test.want) {
				t.Fatalf("maintenanceUpdateArgs() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFilterDaemonFlag(t *testing.T) {
	got := filterDaemonFlag([]string{"--config", "/tmp/mail", "--daemon", "--port", "8123"})
	want := []string{"--config", "/tmp/mail", "--port", "8123"}
	if !slices.Equal(got, want) {
		t.Fatalf("filterDaemonFlag() = %v, want %v", got, want)
	}
}

func TestStatusUsesCLIConfigAndAddressOverrides(t *testing.T) {
	previous := cliOptions
	t.Cleanup(func() { cliOptions = previous })
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server:\n  address: '2001:db8::1'\n  port: 8123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cliOptions = serverOptions{configDir: configDir}
	t.Setenv("PORT", "9000")
	if got, err := statusPort(); err != nil || got != 9000 {
		t.Fatalf("statusPort() = %d, %v; environment value should override file", got, err)
	}
	if got := statusAddress(); got != "2001:db8::1" {
		t.Fatalf("statusAddress() = %q, want configured address", got)
	}
	cliOptions.port = 9443
	cliOptions.address = "[::]"
	if got, err := statusPort(); err != nil || got != 9443 {
		t.Fatalf("statusPort() = %d, %v; CLI value should override environment", got, err)
	}
	if got := statusAddress(); got != "::1" {
		t.Fatalf("statusAddress() = %q, want loopback for wildcard listener", got)
	}
}

func TestStatusPortAndAddressFallbacks(t *testing.T) {
	previous := cliOptions
	t.Cleanup(func() { cliOptions = previous })
	configDir := t.TempDir()
	cliOptions = serverOptions{configDir: configDir}
	t.Setenv("PORT", "")
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := statusPort(); err != nil || got < 64000 || got > 64999 {
		t.Fatalf("invalid config status port fallback = %d, %v", got, err)
	}
	t.Setenv("PORT", "70000")
	if _, err := statusPort(); err == nil || !strings.Contains(err.Error(), "invalid PORT") {
		t.Fatalf("invalid environment status port error = %v", err)
	}
	t.Setenv("PORT", "")
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server:\n  address: '0.0.0.0'\n  port: 64501\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := statusAddress(); got != "127.0.0.1" {
		t.Fatalf("wildcard IPv4 status address = %q", got)
	}
	t.Setenv("LISTEN", "[2001:db8::2]")
	if got := statusAddress(); got != "2001:db8::2" {
		t.Fatalf("environment status address = %q", got)
	}
}

func TestCheckHealthEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		wantOK bool
	}{
		{name: "healthy", status: http.StatusOK, body: `{"status":"healthy"}`, wantOK: true},
		{name: "unhealthy", status: http.StatusServiceUnavailable, body: `{"status":"unhealthy"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := checkHealthEndpoint(context.Background(), server.URL)
			if (err == nil) != tc.wantOK {
				t.Fatalf("checkHealthEndpoint() error = %v, healthy = %t", err, tc.wantOK)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer server.Close()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close health response body: %v", err)
		}
	})
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload["status"] != "healthy" {
		t.Fatalf("health response = %#v, err = %v", payload, err)
	}
}

func TestApplyEnvironmentOverrides(t *testing.T) {
	debugFlag = false
	t.Cleanup(func() { debugFlag = false })
	t.Setenv("LISTEN", "127.0.0.1")
	t.Setenv("PORT", "8080")
	t.Setenv("MODE", "dev")
	t.Setenv("DEBUG", "yes")
	t.Setenv("DOMAIN", "mail.example.test")
	t.Setenv("SMTP_HOST", "relay.example.test")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", "mailer")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_TLS", "auto")
	t.Setenv("SMTP_FROM_NAME", "Mail")
	t.Setenv("SMTP_FROM_EMAIL", "no-reply@mail.example.test")
	cfg := &config.Config{}
	if err := applyEnvironmentOverrides(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != "127.0.0.1" || cfg.Server.Port != 8080 || cfg.Server.Mode != "development" || !cfg.Server.Debug {
		t.Fatalf("environment overrides = %#v", cfg.Server)
	}
	email := cfg.Server.Notifications.Email
	if cfg.Server.FQDN != "mail.example.test" || email.SMTP.Host != "relay.example.test" || email.SMTP.Port != 465 || email.SMTP.Username != "mailer" || email.SMTP.Password != "secret" || email.SMTP.TLS != "auto" || email.From.Name != "Mail" || email.From.Email != "no-reply@mail.example.test" {
		t.Fatalf("SMTP environment overrides = %#v", email)
	}
	t.Setenv("PORT", "bad")
	cfg.Server.Port = 8123
	if err := applyEnvironmentOverrides(cfg); err != nil || cfg.Server.Port != 8123 {
		t.Fatalf("invalid PORT should preserve the configured default: port=%d err=%v", cfg.Server.Port, err)
	}
	cfg.Server.Mode = "production"
	t.Setenv("MODE", "invalid")
	if err := applyEnvironmentOverrides(cfg); err != nil || cfg.Server.Mode != "production" {
		t.Fatalf("invalid MODE should preserve the configured default: mode=%q err=%v", cfg.Server.Mode, err)
	}
	t.Setenv("SMTP_PORT", "bad")
	t.Setenv("SMTP_TLS", "invalid")
	if err := applyEnvironmentOverrides(cfg); err != nil || cfg.Server.Notifications.Email.SMTP.Port != 587 || cfg.Server.Notifications.Email.SMTP.TLS != "auto" {
		t.Fatalf("invalid SMTP values should use defaults: %+v err=%v", cfg.Server.Notifications.Email.SMTP, err)
	}
}

func TestDebugFlagOverridesEnvironmentAndModeDefaults(t *testing.T) {
	previous := debugFlag
	t.Cleanup(func() { debugFlag = previous })
	t.Setenv("MODE", "debug")
	t.Setenv("DEBUG", "false")
	cfg := &config.Config{}
	if err := applyEnvironmentOverrides(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Debug {
		t.Fatal("DEBUG=false did not disable debug endpoints in debug mode")
	}
	debugFlag = true
	if err := applyEnvironmentOverrides(cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Server.Debug {
		t.Fatal("explicit --debug flag did not override DEBUG=false")
	}
}

func TestSMTPFactsPreferDomainOverride(t *testing.T) {
	t.Setenv("DOMAIN", "mail.example.test")
	t.Setenv("HOSTNAME", "ignored.example.test")
	facts := smtpFacts()
	if facts.FQDN != "mail.example.test" {
		t.Fatalf("smtpFacts FQDN = %q, want DOMAIN override", facts.FQDN)
	}
}

func TestParseLinuxDefaultGateway(t *testing.T) {
	routes := "Iface Destination Gateway Flags RefCnt Use Metric Mask\n" +
		"eth0 00000000 0102A8C0 0003 0 0 100 00000000\n" +
		"eth1 00000000 0202A8C0 0000 0 0 200 00000000\n"
	if got := parseLinuxDefaultGateway(routes); got != "192.168.2.1" {
		t.Fatalf("default gateway = %q, want 192.168.2.1", got)
	}
	if got := parseLinuxDefaultGateway("Iface Destination Gateway Flags\neth0 00000001 0102A8C0 0003\n"); got != "" {
		t.Fatalf("non-default route yielded gateway %q", got)
	}
}
