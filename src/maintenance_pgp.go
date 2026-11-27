package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
	"github.com/apimgr/mail/src/service"
)

func handlePGPMaintenance(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		printPGPMaintenanceHelp(os.Stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "generate":
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp generate\n", binaryName)
			return 2
		}
		return handlePGPGenerate()
	case "publish":
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp publish\n", binaryName)
			return 2
		}
		return handlePGPPublish()
	case "rotate":
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp rotate\n", binaryName)
			return 2
		}
		return handlePGPRotate()
	case "delete":
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp delete\n", binaryName)
			return 2
		}
		return handlePGPDelete()
	case "import":
		if len(args) != 2 {
			fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp import FILE\n", binaryName)
			return 2
		}
		return handlePGPImport(args[1])
	case "export":
		return handlePGPExport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "%s: unknown PGP maintenance action %q\n", binaryName, args[0])
		return 2
	}
}

func printPGPMaintenanceHelp(writer io.Writer) {
	_, _ = fmt.Fprintf(writer, "Usage: %s --maintenance pgp <generate|rotate|publish|export|import|delete> [FILE]\n", binaryName)
	_, _ = fmt.Fprintln(writer, "  generate             Generate the project security keypair")
	_, _ = fmt.Fprintln(writer, "  rotate               Rotate the project key and re-encrypt retained reports")
	_, _ = fmt.Fprintln(writer, "  publish              Publish the public key to configured keyservers")
	_, _ = fmt.Fprintln(writer, "  export public [FILE]  Write the ASCII-armored public key to FILE or stdout")
	_, _ = fmt.Fprintln(writer, "  export private FILE   Export the decrypted private key after confirmation")
	_, _ = fmt.Fprintln(writer, "  import FILE           Import an existing private key after confirmation")
	_, _ = fmt.Fprintln(writer, "  delete               Remove the active keypair after confirmation")
}

func handlePGPGenerate() int {
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	if err := logger.Init(loggerConfig(cfg.Server.Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()

	metadata, err := db.GetPGPKeyMetadata(context.Background())
	switch {
	case err == nil && !metadata.Revoked:
		fmt.Fprintln(os.Stderr, "a project PGP keypair already exists; refusing to overwrite it")
		return 1
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		fmt.Fprintf(os.Stderr, "%s: read PGP key metadata: %v\n", binaryName, err)
		return 1
	}

	securityEmail, err := configuredSecurityEmail(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: security contact: %v\n", binaryName, err)
		return 1
	}
	created := time.Now().UTC().Truncate(time.Second)
	publicArmor, privateArmor, fingerprint, expires, err := security.GeneratePGPKey("Mail Security <"+securityEmail+">", created)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: generate PGP keypair: %v\n", binaryName, err)
		return 1
	}
	installationSecret, err := db.GeneratedSecret(context.Background(), "installation_secret")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read installation secret: %v\n", binaryName, err)
		return 1
	}
	encryptedPrivate, err := security.EncryptPGPPrivateKey(privateArmor, installationSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: encrypt PGP private key: %v\n", binaryName, err)
		return 1
	}

	keyDir := filepath.Join(appPaths.Config, "security")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "%s: create PGP key directory: %v\n", binaryName, err)
		return 1
	}
	if err := os.Chmod(keyDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "%s: secure PGP key directory: %v\n", binaryName, err)
		return 1
	}
	privatePath := filepath.Join(keyDir, "pgp.priv.asc.enc")
	publicPath := filepath.Join(keyDir, "pgp.pub.asc")
	if fileExists(privatePath) || fileExists(publicPath) {
		fmt.Fprintln(os.Stderr, "PGP key files already exist; refusing to overwrite them")
		return 1
	}

	if err := writeExclusiveFile(privatePath, encryptedPrivate); err != nil {
		fmt.Fprintf(os.Stderr, "%s: store encrypted PGP private key: %v\n", binaryName, err)
		return 1
	}
	metadata = database.PGPKeyMetadata{
		Fingerprint:         fingerprint,
		CreatedAt:           created,
		ExpiresAt:           expires,
		KeyserversPublished: []database.PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		_ = os.Remove(privatePath)
		fmt.Fprintf(os.Stderr, "%s: save PGP key metadata: %v\n", binaryName, err)
		return 1
	}
	if err := writeExclusiveFile(publicPath, publicArmor); err != nil {
		_ = os.Remove(privatePath)
		metadata.Revoked = true
		_ = db.SavePGPKeyMetadata(context.Background(), metadata)
		fmt.Fprintf(os.Stderr, "%s: store PGP public key: %v\n", binaryName, err)
		return 1
	}
	if err := db.AppendAuditEvent(context.Background(), database.AuditEvent{
		Category: "security", Action: "pgp_generated", ActorType: "operator", TargetType: "pgp_key",
		TargetID: fingerprint, Details: map[string]any{"expires_at": expires.Format(time.RFC3339)}, Success: true,
	}); err != nil {
		logger.Security("security.pgp_generated_audit_failed", "local")
		fmt.Fprintf(os.Stderr, "%s: record PGP generation audit event: %v\n", binaryName, err)
		return 1
	}
	logger.Audit("security.pgp_generated", "operator", map[string]interface{}{"fingerprint": fingerprint, "expires_at": expires.Format(time.RFC3339)})
	if err := service.PublishPGPKeyservers(context.Background(), db, keyDir, cfg.Web.Security.Keyservers, time.Now().UTC(), false); err != nil {
		logger.Warnf("PGP keyserver publication is pending or incomplete: %v", err)
	}
	fmt.Printf("PGP keypair generated. Fingerprint: %s\nExpires: %s\n", fingerprint, expires.Format(time.RFC3339))
	return 0
}

func handlePGPImport(importPath string) int {
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	input := bufio.NewReader(os.Stdin)
	if _, confirmed := readPGPSensitiveConfirmation(input, os.Stderr, "IMPORT PGP PRIVATE KEY", false); !confirmed {
		fmt.Fprintln(os.Stderr, "PGP key import was not confirmed")
		return 77
	}
	securityEmail, err := configuredSecurityEmail(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: security contact: %v\n", binaryName, err)
		return 1
	}
	info, err := os.Stat(importPath)
	if err != nil || info.Size() <= 0 || info.Size() > 1<<20 {
		fmt.Fprintf(os.Stderr, "%s: imported private key file is missing or exceeds the 1 MiB limit\n", binaryName)
		return 1
	}
	privateArmor, err := os.ReadFile(importPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read imported private key: %v\n", binaryName, err)
		return 1
	}
	importedKey, err := inspectPGPImport(privateArmor, securityEmail, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: validate imported private key: %v\n", binaryName, err)
		return 1
	}
	identityMatches := importedKey.identityMatches
	if !identityMatches {
		fmt.Fprintf(os.Stderr, "Warning: imported identity does not match Mail Security <%s>.\n", securityEmail)
		if _, confirmed := readPGPSensitiveConfirmation(input, os.Stderr, "IMPORT ANYWAY", false); !confirmed {
			fmt.Fprintln(os.Stderr, "identity mismatch override was not confirmed")
			return 77
		}
	}
	if !maintenanceCallerElevated() && !authorizeMaintenanceWithReader(cfg, input) {
		fmt.Fprintln(os.Stderr, "operator token confirmation failed")
		return 77
	}
	unlock := security.WithPGPKeyWriteLock()
	defer unlock()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	securityDir := filepath.Join(appPaths.Config, "security")
	if err := storePGPImport(ctx, db, securityDir, privateArmor, importedKey); err != nil {
		fmt.Fprintf(os.Stderr, "%s: store imported PGP key: %v\n", binaryName, err)
		return 1
	}
	operator := currentOperatorName()
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "pgp_imported", ActorType: "operator", ActorID: operator,
		ActorIP: "local", TargetType: "pgp_key", TargetID: importedKey.fingerprint,
		Details: map[string]any{"identity_override": !identityMatches}, Success: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s: key imported but audit logging failed: %v\n", binaryName, err)
		return 1
	}
	if err := service.PublishPGPKeyservers(ctx, db, securityDir, cfg.Web.Security.Keyservers, time.Now().UTC(), false); err != nil {
		fmt.Fprintf(os.Stderr, "PGP keyserver publication is pending or incomplete: %v\n", err)
	}
	fmt.Printf("PGP keypair imported. Fingerprint: %s\nExpires: %s\n", importedKey.fingerprint, importedKey.expires.Format(time.RFC3339))
	return 0
}

func storePGPImport(ctx context.Context, db *database.DB, securityDir string, privateArmor []byte, importedKey inspectedPGPImport) error {
	if db == nil {
		return fmt.Errorf("database is unavailable")
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err == nil && !metadata.Revoked {
		return fmt.Errorf("an active PGP keypair already exists; rotate it instead of importing over it")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read PGP key metadata: %w", err)
	}
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		return fmt.Errorf("create PGP key directory: %w", err)
	}
	if err := os.Chmod(securityDir, 0o700); err != nil {
		return fmt.Errorf("secure PGP key directory: %w", err)
	}
	privatePath := filepath.Join(securityDir, "pgp.priv.asc.enc")
	publicPath := filepath.Join(securityDir, "pgp.pub.asc")
	if fileExists(privatePath) || fileExists(publicPath) {
		return fmt.Errorf("PGP key files already exist; refusing to overwrite them")
	}
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		return fmt.Errorf("read installation secret: %w", err)
	}
	encryptedPrivate, err := security.EncryptPGPPrivateKey(privateArmor, installationSecret)
	if err != nil {
		return fmt.Errorf("encrypt imported private key: %w", err)
	}
	if err := writeExclusiveFile(privatePath, encryptedPrivate); err != nil {
		return fmt.Errorf("store imported private key: %w", err)
	}
	if err := writeExclusiveFile(publicPath, importedKey.publicArmor); err != nil {
		_ = os.Remove(privatePath)
		return fmt.Errorf("store imported public key: %w", err)
	}
	metadata = database.PGPKeyMetadata{
		Fingerprint: importedKey.fingerprint, CreatedAt: importedKey.created, ExpiresAt: importedKey.expires,
		KeyserversPublished: []database.PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		_ = os.Remove(privatePath)
		_ = os.Remove(publicPath)
		return fmt.Errorf("save imported PGP metadata: %w", err)
	}
	return nil
}

type inspectedPGPImport struct {
	publicArmor     []byte
	fingerprint     string
	created         time.Time
	expires         time.Time
	identityMatches bool
}

func inspectPGPImport(privateArmor []byte, expectedEmail string, now time.Time) (inspectedPGPImport, error) {
	publicArmor, fingerprint, identities, created, expires, err := security.InspectPGPPrivateKey(privateArmor)
	if err != nil {
		return inspectedPGPImport{}, err
	}
	if len(identities) == 0 || created.After(now) || !expires.After(now) {
		return inspectedPGPImport{}, fmt.Errorf("imported key has no identity or is not currently valid")
	}
	return inspectedPGPImport{
		publicArmor: publicArmor, fingerprint: fingerprint, created: created, expires: expires,
		identityMatches: pgpIdentityMatches(identities, expectedEmail),
	}, nil
}

func pgpIdentityMatches(identities []string, expectedEmail string) bool {
	for _, identity := range identities {
		address, err := mail.ParseAddress(identity)
		if err == nil && address.Name == "Mail Security" && strings.EqualFold(address.Address, expectedEmail) {
			return true
		}
	}
	return false
}

func handlePGPPublish() int {
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	if err := logger.Init(loggerConfig(cfg.Server.Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	securityDir := filepath.Join(appPaths.Config, "security")
	if err := service.PublishPGPKeyservers(context.Background(), db, securityDir, cfg.Web.Security.Keyservers, time.Now().UTC(), false); err != nil {
		fmt.Fprintf(os.Stderr, "%s: keyserver publication incomplete: %v\n", binaryName, err)
		return 1
	}
	fmt.Println("PGP keyserver publication complete")
	return 0
}

func configuredSecurityEmail(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("configuration is unavailable")
	}
	value := strings.TrimSpace(cfg.Server.Contact.Security.Email)
	if value == "" {
		return "", fmt.Errorf("server.contact.security.email must be set to generate the public key identity")
	}
	host := strings.TrimSuffix(strings.TrimSpace(cfg.Server.FQDN), ".")
	if strings.Contains(value, "{fqdn}") && host == "" {
		return "", fmt.Errorf("server.fqdn must be set to resolve server.contact.security.email")
	}
	value = strings.ReplaceAll(value, "{fqdn}", host)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return "", fmt.Errorf("server.contact.security.email must resolve to one valid email address")
	}
	return value, nil
}

func handlePGPExport(args []string) int {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Printf("Usage: %s --maintenance pgp export <public [FILE]|private FILE>\n", binaryName)
		return 0
	}
	if len(args) > 0 && args[0] == "private" {
		return handlePGPPrivateExport(args[1:])
	}
	if len(args) < 1 || args[0] != "public" || len(args) > 2 {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp export public [FILE]\n", binaryName)
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	publicPath := filepath.Join(appPaths.Config, "security", "pgp.pub.asc")
	publicArmor, err := os.ReadFile(publicPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read public PGP key: %v\n", binaryName, err)
		return 66
	}
	fingerprint, err := security.PGPFingerprint(publicArmor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: stored public PGP key is invalid\n", binaryName)
		return 1
	}
	metadata, err := db.GetPGPKeyMetadata(context.Background())
	if err != nil || metadata.Revoked || metadata.Fingerprint != fingerprint {
		fmt.Fprintf(os.Stderr, "%s: PGP key metadata does not match the stored public key\n", binaryName)
		return 1
	}
	if len(args) == 1 {
		if _, err := os.Stdout.Write(publicArmor); err != nil {
			fmt.Fprintf(os.Stderr, "%s: write public PGP key: %v\n", binaryName, err)
			return 1
		}
		return 0
	}
	if err := writeExclusiveFile(args[1], publicArmor); err != nil {
		fmt.Fprintf(os.Stderr, "%s: export public PGP key: %v\n", binaryName, err)
		return 1
	}
	return 0
}

func handlePGPPrivateExport(args []string) int {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance pgp export private FILE\n", binaryName)
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	input := bufio.NewReader(os.Stdin)
	reason, confirmed := readPGPSensitiveConfirmation(input, os.Stderr, "EXPORT PRIVATE PGP KEY", true)
	if !confirmed {
		fmt.Fprintln(os.Stderr, "private-key export was not confirmed")
		return 77
	}
	if !maintenanceCallerElevated() && !authorizeMaintenanceWithReader(cfg, input) {
		fmt.Fprintln(os.Stderr, "operator token confirmation failed")
		return 77
	}
	operator := currentOperatorName()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if err := exportPGPPrivateKey(ctx, db, appPaths.Config, args[0], operator, reason, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: export private PGP key: %v\n", binaryName, err)
		return 1
	}
	_, _ = fmt.Fprintf(os.Stdout, "Private PGP key exported to %s\n", args[0])
	return 0
}

func exportPGPPrivateKey(ctx context.Context, db *database.DB, configDir, outputPath, operator, reason string, now time.Time) error {
	if db == nil || db.ServerReadDB == nil || db.ServerDB == nil || strings.TrimSpace(operator) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(outputPath) == "" {
		return errors.New("database, operator, reason, and output path are required")
	}
	var recent int
	if err := db.ServerReadDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log
		WHERE category = 'security' AND action = 'private_key_exported' AND actor_id = ? AND timestamp >= ?`, operator, now.UTC().Add(-time.Hour).Unix()).Scan(&recent); err != nil {
		return fmt.Errorf("check private-key export rate limit: %w", err)
	}
	if recent > 0 {
		return errors.New("private-key export is limited to one attempt per hour per operator")
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil || metadata.Revoked || !metadata.ExpiresAt.After(now.UTC()) {
		return errors.New("no active PGP private key is available")
	}
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		return fmt.Errorf("read installation secret: %w", err)
	}
	privatePath := filepath.Join(configDir, "security", "pgp.priv.asc.enc")
	encryptedPrivate, err := os.ReadFile(privatePath)
	if err != nil {
		return fmt.Errorf("read encrypted PGP private key: %w", err)
	}
	privateArmor, err := security.DecryptPGPPrivateKey(encryptedPrivate, installationSecret)
	if err != nil {
		return fmt.Errorf("decrypt PGP private key: %w", err)
	}
	privateFingerprint, err := security.PGPPrivateFingerprint(privateArmor)
	if err != nil || privateFingerprint != metadata.Fingerprint {
		return errors.New("decrypted private PGP key does not match active metadata")
	}
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "private_key_exported", ActorType: "operator", ActorID: operator,
		ActorIP: "local", TargetType: "pgp_key", TargetID: metadata.Fingerprint,
		Details: map[string]any{"reason": reason, "path": filepath.Base(outputPath)}, Success: false,
	}); err != nil {
		return fmt.Errorf("record private-key export attempt: %w", err)
	}
	if err := writeExclusiveFile(outputPath, privateArmor); err != nil {
		return fmt.Errorf("export private PGP key: %w", err)
	}
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "private_key_exported", ActorType: "operator", ActorID: operator,
		ActorIP: "local", TargetType: "pgp_key", TargetID: metadata.Fingerprint,
		Details: map[string]any{"reason": reason, "path": filepath.Base(outputPath)}, Success: true,
	}); err != nil {
		return fmt.Errorf("export completed but audit logging failed: %w", err)
	}
	return nil
}

func handlePGPDelete() int {
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance pgp requires the operator token or an elevated account")
		return 77
	}
	fmt.Fprintln(os.Stderr, "Warning: deleting this key makes retained PGP-encrypted security reports unreadable.")
	input := bufio.NewReader(os.Stdin)
	if _, confirmed := readPGPSensitiveConfirmation(input, os.Stderr, "DELETE PGP KEY", false); !confirmed {
		fmt.Fprintln(os.Stderr, "PGP key deletion was not confirmed")
		return 77
	}
	if !maintenanceCallerElevated() && !authorizeMaintenanceWithReader(cfg, input) {
		fmt.Fprintln(os.Stderr, "operator token confirmation failed")
		return 77
	}
	unlock := security.WithPGPKeyWriteLock()
	defer unlock()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read installation secret: %v\n", binaryName, err)
		return 1
	}
	securityDir := filepath.Join(appPaths.Config, "security")
	if err := recoverPGPRotationFiles(ctx, db, securityDir, installationSecret); err != nil {
		fmt.Fprintf(os.Stderr, "%s: recover pending PGP rotation: %v\n", binaryName, err)
		return 1
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil || metadata.Revoked {
		fmt.Fprintf(os.Stderr, "%s: no active PGP keypair is available\n", binaryName)
		return 1
	}
	operator := currentOperatorName()
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "pgp_deleted", ActorType: "operator", ActorID: operator,
		ActorIP: "local", TargetType: "pgp_key", TargetID: metadata.Fingerprint,
		Details: map[string]any{"warning_acknowledged": true}, Success: false,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s: record PGP deletion attempt: %v\n", binaryName, err)
		return 1
	}
	if err := deletePGPKeyFiles(ctx, db, securityDir, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "%s: delete PGP keypair: %v\n", binaryName, err)
		return 1
	}
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "pgp_deleted", ActorType: "operator", ActorID: operator,
		ActorIP: "local", TargetType: "pgp_key", TargetID: metadata.Fingerprint,
		Details: map[string]any{"warning_acknowledged": true}, Success: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s: PGP key deleted but audit logging failed: %v\n", binaryName, err)
		return 1
	}
	fmt.Println("PGP keypair deleted; its fingerprint remains in database audit metadata")
	return 0
}

func deletePGPKeyFiles(ctx context.Context, db *database.DB, securityDir string, metadata database.PGPKeyMetadata) error {
	stamp := time.Now().UTC().UnixNano()
	paths := []string{
		filepath.Join(securityDir, "pgp.priv.asc.enc"),
		filepath.Join(securityDir, "pgp.pub.asc"),
		filepath.Join(securityDir, "pgp.priv.old.asc.enc"),
		filepath.Join(securityDir, "pgp.priv.asc.enc.new"),
		filepath.Join(securityDir, "pgp.pub.asc.new"),
	}
	type stagedKeyFile struct{ original, staged string }
	var staged []stagedKeyFile
	rollback := func() {
		for index := len(staged) - 1; index >= 0; index-- {
			_ = os.Rename(staged[index].staged, staged[index].original)
		}
	}
	for index, path := range paths {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			rollback()
			return fmt.Errorf("inspect PGP key file: %w", err)
		}
		stagedPath := fmt.Sprintf("%s.deleting-%d-%d", path, stamp, index)
		if err := os.Rename(path, stagedPath); err != nil {
			rollback()
			return fmt.Errorf("stage PGP key deletion: %w", err)
		}
		staged = append(staged, stagedKeyFile{original: path, staged: stagedPath})
	}
	metadata.Revoked = true
	if err := db.SavePGPKeyMetadata(ctx, metadata); err != nil {
		rollback()
		return fmt.Errorf("record revoked PGP metadata: %w", err)
	}
	for _, file := range staged {
		if err := os.Remove(file.staged); err != nil {
			return fmt.Errorf("remove staged PGP key file: %w", err)
		}
	}
	_ = os.Remove(filepath.Join(securityDir, "keyservers.state"))
	return nil
}

func readPGPSensitiveConfirmation(input *bufio.Reader, output io.Writer, expected string, requireReason bool) (string, bool) {
	_, _ = fmt.Fprintf(output, "Type %q to continue: ", expected)
	confirmation, err := input.ReadString('\n')
	if err != nil && len(confirmation) == 0 || strings.TrimSpace(confirmation) != expected {
		return "", false
	}
	if !requireReason {
		return "", true
	}
	_, _ = fmt.Fprint(output, "Reason for this operation: ")
	reason, err := input.ReadString('\n')
	if err != nil && len(reason) == 0 {
		return "", false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 || strings.ContainsAny(reason, "\r\n\x00") {
		return "", false
	}
	return reason, true
}

func maintenanceCallerElevated() bool {
	manager, err := newServiceManager()
	return err == nil && manager.IsElevated()
}

func currentOperatorName() string {
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		return strings.TrimSpace(current.Username)
	}
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return name
	}
	if name := strings.TrimSpace(os.Getenv("USERNAME")); name != "" {
		return name
	}
	return "unknown"
}

func writeExclusiveFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".pgp-key-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
