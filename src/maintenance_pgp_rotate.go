package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
	"github.com/apimgr/mail/src/service"
)

func handlePGPRotate() int {
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
	if _, confirmed := readPGPSensitiveConfirmation(input, os.Stderr, "ROTATE PGP KEY", false); !confirmed {
		fmt.Fprintln(os.Stderr, "PGP key rotation was not confirmed")
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
	securityDir := filepath.Join(appPaths.Config, "security")
	installationSecret, err := db.GeneratedSecret(ctx, "installation_secret")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read installation secret: %v\n", binaryName, err)
		return 1
	}
	if err := recoverPGPRotationFiles(ctx, db, securityDir, installationSecret); err != nil {
		fmt.Fprintf(os.Stderr, "%s: recover pending PGP rotation: %v\n", binaryName, err)
		return 1
	}
	securityEmail, err := configuredSecurityEmail(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: security contact: %v\n", binaryName, err)
		return 1
	}
	metadata, err := rotatePGPKey(ctx, db, securityDir, installationSecret, "Mail Security <"+securityEmail+">", time.Now().UTC().Truncate(time.Second))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: rotate PGP key: %v\n", binaryName, err)
		return 1
	}
	if err := service.PublishPGPKeyservers(ctx, db, securityDir, cfg.Web.Security.Keyservers, time.Now().UTC(), false); err != nil {
		logger.Warnf("PGP keyserver publication is pending or incomplete: %v", err)
	}
	if err := db.AppendAuditEvent(ctx, database.AuditEvent{
		Category: "security", Action: "pgp_rotated", ActorType: "operator", ActorID: currentOperatorName(),
		ActorIP: "local", TargetType: "pgp_key", TargetID: metadata.Fingerprint,
		Details: map[string]any{"last_rotated_at": metadata.LastRotatedAt.Format(time.RFC3339)}, Success: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s: key rotated but audit logging failed: %v\n", binaryName, err)
		return 1
	}
	fmt.Printf("PGP keypair rotated. Fingerprint: %s\nRetired key expires: %s\n", metadata.Fingerprint, metadata.LastRotatedAt.Add(30*24*time.Hour).Format(time.RFC3339))
	return 0
}

func rotatePGPKey(ctx context.Context, db *database.DB, securityDir string, installationSecret []byte, identity string, now time.Time) (database.PGPKeyMetadata, error) {
	if db == nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("database is unavailable")
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("read active PGP key metadata: %w", err)
	}
	if metadata.Revoked {
		return database.PGPKeyMetadata{}, fmt.Errorf("active PGP keypair is revoked")
	}
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("create PGP key directory: %w", err)
	}
	if err := os.Chmod(securityDir, 0o700); err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("secure PGP key directory: %w", err)
	}
	privatePath := filepath.Join(securityDir, "pgp.priv.asc.enc")
	publicPath := filepath.Join(securityDir, "pgp.pub.asc")
	encryptedOldPrivate, err := os.ReadFile(privatePath)
	if err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("read active encrypted PGP private key: %w", err)
	}
	oldPrivate, err := security.DecryptPGPPrivateKey(encryptedOldPrivate, installationSecret)
	if err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("decrypt active PGP private key: %w", err)
	}
	oldFingerprint, err := security.PGPPrivateFingerprint(oldPrivate)
	if err != nil || oldFingerprint != metadata.Fingerprint {
		return database.PGPKeyMetadata{}, fmt.Errorf("active PGP private key does not match database metadata")
	}
	oldPublic, err := os.ReadFile(publicPath)
	if err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("read active PGP public key: %w", err)
	}
	oldPublicFingerprint, err := security.PGPFingerprint(oldPublic)
	if err != nil || oldPublicFingerprint != metadata.Fingerprint {
		return database.PGPKeyMetadata{}, fmt.Errorf("active PGP public key does not match database metadata")
	}
	newPublic, newPrivate, fingerprint, expires, err := security.GeneratePGPKey(identity, now)
	if err != nil {
		return database.PGPKeyMetadata{}, err
	}
	signedPublic, err := security.SignPGPPublicKey(newPublic, oldPrivate)
	if err != nil {
		return database.PGPKeyMetadata{}, err
	}
	if signedFingerprint, err := security.PGPFingerprint(signedPublic); err != nil || signedFingerprint != fingerprint {
		return database.PGPKeyMetadata{}, fmt.Errorf("signed rotated public key fingerprint changed")
	}
	encryptedNewPrivate, err := security.EncryptPGPPrivateKey(newPrivate, installationSecret)
	if err != nil {
		return database.PGPKeyMetadata{}, err
	}
	stagedPrivate := filepath.Join(securityDir, "pgp.priv.asc.enc.new")
	stagedPublic := filepath.Join(securityDir, "pgp.pub.asc.new")
	if fileExists(stagedPrivate) || fileExists(stagedPublic) {
		return database.PGPKeyMetadata{}, fmt.Errorf("staged PGP rotation files already exist")
	}
	if err := writeExclusiveFile(stagedPrivate, encryptedNewPrivate); err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("stage new encrypted PGP private key: %w", err)
	}
	if err := writeExclusiveFile(stagedPublic, signedPublic); err != nil {
		_ = os.Remove(stagedPrivate)
		return database.PGPKeyMetadata{}, fmt.Errorf("stage new signed PGP public key: %w", err)
	}
	rotatedAt := now.UTC().Truncate(time.Second)
	newMetadata := database.PGPKeyMetadata{
		Fingerprint: fingerprint, CreatedAt: rotatedAt, ExpiresAt: expires, LastRotatedAt: &rotatedAt,
		KeyserversPublished: []database.PGPKeyserverPublication{},
	}
	if err := db.RotatePGPKeyAndReports(ctx, newMetadata, func(ciphertext []byte) ([]byte, error) {
		plaintext, err := security.DecryptPGPMessage(oldPrivate, ciphertext)
		if err != nil {
			return nil, err
		}
		return security.EncryptPGPMessage(signedPublic, plaintext)
	}); err != nil {
		_ = os.Remove(stagedPrivate)
		_ = os.Remove(stagedPublic)
		return database.PGPKeyMetadata{}, fmt.Errorf("commit PGP report re-encryption and key metadata: %w", err)
	}
	if err := finishPGPRotationFiles(ctx, db, securityDir, installationSecret); err != nil {
		return database.PGPKeyMetadata{}, fmt.Errorf("rotation committed; finish staged key installation: %w", err)
	}
	return newMetadata, nil
}

func recoverPGPRotationFiles(ctx context.Context, db *database.DB, securityDir string, installationSecret []byte) error {
	return finishPGPRotationFiles(ctx, db, securityDir, installationSecret)
}

func finishPGPRotationFiles(ctx context.Context, db *database.DB, securityDir string, installationSecret []byte) error {
	stagedPrivate := filepath.Join(securityDir, "pgp.priv.asc.enc.new")
	stagedPublic := filepath.Join(securityDir, "pgp.pub.asc.new")
	privateExists, publicExists := fileExists(stagedPrivate), fileExists(stagedPublic)
	if !privateExists && !publicExists {
		return nil
	}
	metadata, err := db.GetPGPKeyMetadata(ctx)
	if err != nil {
		return fmt.Errorf("read rotation metadata: %w", err)
	}
	if !privateExists || !publicExists {
		activePublic, publicErr := os.ReadFile(filepath.Join(securityDir, "pgp.pub.asc"))
		activePublicFingerprint, publicFingerprintErr := security.PGPFingerprint(activePublic)
		activePrivate, privateErr := os.ReadFile(filepath.Join(securityDir, "pgp.priv.asc.enc"))
		activePrivateArmor, privateDecryptErr := security.DecryptPGPPrivateKey(activePrivate, installationSecret)
		activePrivateFingerprint, privateFingerprintErr := security.PGPPrivateFingerprint(activePrivateArmor)
		if publicErr == nil && publicFingerprintErr == nil && activePublicFingerprint == metadata.Fingerprint && privateErr == nil && privateDecryptErr == nil && privateFingerprintErr == nil && activePrivateFingerprint == metadata.Fingerprint {
			return removePGPRotationStages(stagedPrivate, stagedPublic)
		}
		if publicExists {
			staged, readErr := os.ReadFile(stagedPublic)
			stagedFingerprint, fingerprintErr := security.PGPFingerprint(staged)
			if readErr == nil && fingerprintErr == nil && stagedFingerprint == metadata.Fingerprint {
				return fmt.Errorf("committed PGP rotation is missing its staged private key")
			}
		}
		if privateExists {
			staged, readErr := os.ReadFile(stagedPrivate)
			stagedArmor, decryptErr := security.DecryptPGPPrivateKey(staged, installationSecret)
			stagedFingerprint, fingerprintErr := security.PGPPrivateFingerprint(stagedArmor)
			if readErr == nil && decryptErr == nil && fingerprintErr == nil && stagedFingerprint == metadata.Fingerprint {
				return fmt.Errorf("committed PGP rotation is missing its staged public key")
			}
		}
		return removePGPRotationStages(stagedPrivate, stagedPublic)
	}
	stagedPublicArmor, err := os.ReadFile(stagedPublic)
	if err != nil {
		return fmt.Errorf("read staged public PGP key: %w", err)
	}
	stagedFingerprint, err := security.PGPFingerprint(stagedPublicArmor)
	if err != nil {
		return fmt.Errorf("validate staged public PGP key: %w", err)
	}
	if stagedFingerprint != metadata.Fingerprint {
		stagedPrivateCiphertext, readErr := os.ReadFile(stagedPrivate)
		stagedPrivateArmor, decryptErr := security.DecryptPGPPrivateKey(stagedPrivateCiphertext, installationSecret)
		stagedPrivateFingerprint, fingerprintErr := security.PGPPrivateFingerprint(stagedPrivateArmor)
		if readErr == nil && decryptErr == nil && fingerprintErr == nil && stagedPrivateFingerprint == metadata.Fingerprint {
			return fmt.Errorf("committed PGP rotation has a staged public key that does not match metadata")
		}
		return removePGPRotationStages(stagedPrivate, stagedPublic)
	}
	stagedPrivateCiphertext, err := os.ReadFile(stagedPrivate)
	if err != nil {
		return fmt.Errorf("read staged encrypted PGP private key: %w", err)
	}
	stagedPrivateArmor, err := security.DecryptPGPPrivateKey(stagedPrivateCiphertext, installationSecret)
	if err != nil {
		return fmt.Errorf("decrypt staged PGP private key: %w", err)
	}
	privateFingerprint, err := security.PGPPrivateFingerprint(stagedPrivateArmor)
	if err != nil || privateFingerprint != metadata.Fingerprint {
		return fmt.Errorf("staged PGP private key does not match committed metadata")
	}
	privatePath := filepath.Join(securityDir, "pgp.priv.asc.enc")
	oldPath := filepath.Join(securityDir, "pgp.priv.old.asc.enc")
	activeCiphertext, activeErr := os.ReadFile(privatePath)
	if activeErr == nil {
		activeArmor, decryptErr := security.DecryptPGPPrivateKey(activeCiphertext, installationSecret)
		if decryptErr != nil {
			return fmt.Errorf("decrypt current PGP private key during recovery: %w", decryptErr)
		}
		activeFingerprint, fingerprintErr := security.PGPPrivateFingerprint(activeArmor)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		if activeFingerprint != metadata.Fingerprint {
			if err := replacePGPKeyFile(oldPath, activeCiphertext); err != nil {
				return fmt.Errorf("retain old encrypted PGP private key: %w", err)
			}
		}
	} else if !errors.Is(activeErr, os.ErrNotExist) {
		return fmt.Errorf("read current encrypted PGP private key during recovery: %w", activeErr)
	} else if !fileExists(oldPath) {
		return fmt.Errorf("current and retired PGP private keys are both missing")
	}
	if err := replacePGPKeyFile(privatePath, stagedPrivateCiphertext); err != nil {
		return fmt.Errorf("install rotated encrypted PGP private key: %w", err)
	}
	if err := replacePGPKeyFile(filepath.Join(securityDir, "pgp.pub.asc"), stagedPublicArmor); err != nil {
		return fmt.Errorf("install rotated PGP public key: %w", err)
	}
	if err := os.Remove(stagedPrivate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged encrypted PGP private key: %w", err)
	}
	if err := os.Remove(stagedPublic); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged PGP public key: %w", err)
	}
	return nil
}

func removePGPRotationStages(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove uncommitted PGP rotation stage %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func replacePGPKeyFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pgp-rotate-*")
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
	if err := os.Rename(temporaryPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		if retryErr := os.Rename(temporaryPath, path); retryErr != nil {
			return retryErr
		}
	}
	return nil
}
