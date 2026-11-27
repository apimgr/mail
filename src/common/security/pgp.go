package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"golang.org/x/crypto/hkdf"
)

const (
	pgpPrivateCipherPrefix = "pgp-priv:v1:"
	pgpPrivateKeyInfo      = "apimgr/mail/pgp-private-key/aes-256-gcm/v1"
	pgpPrivateKeySalt      = "apimgr/mail/pgp-private-key/kdf/v1"
)

var ErrInvalidPGPKey = errors.New("invalid OpenPGP v4 key")

var pgpKeyLock sync.RWMutex

// WithPGPKeyReadLock keeps key use and report persistence stable during rotation.
func WithPGPKeyReadLock() func() {
	pgpKeyLock.RLock()
	return pgpKeyLock.RUnlock
}

// WithPGPKeyWriteLock excludes key use while the active keypair is rotated.
func WithPGPKeyWriteLock() func() {
	pgpKeyLock.Lock()
	return pgpKeyLock.Unlock
}

// GeneratePGPKey creates a v4 Ed25519 signing key with a Curve25519
// encryption subkey from an identity such as `Mail Security <security@example.net>`.
// The key lifetime is two years from created.
func GeneratePGPKey(identity string, created time.Time) (publicArmor, privateArmor []byte, fingerprint string, expires time.Time, err error) {
	address, err := mail.ParseAddress(strings.TrimSpace(identity))
	if err != nil || address.Name == "" || address.Address == "" {
		return nil, nil, "", time.Time{}, fmt.Errorf("invalid OpenPGP identity")
	}
	created = created.UTC().Truncate(time.Second)
	if created.IsZero() {
		return nil, nil, "", time.Time{}, fmt.Errorf("OpenPGP creation time is required")
	}
	expires = created.AddDate(2, 0, 0)
	lifetime := uint32(expires.Sub(created).Seconds())
	config := &packet.Config{
		Algorithm:       packet.PubKeyAlgoEdDSA,
		Curve:           packet.Curve25519,
		KeyLifetimeSecs: lifetime,
		Time:            func() time.Time { return created },
	}
	entity, err := openpgp.NewEntity(address.Name, "", address.Address, config)
	if err != nil {
		return nil, nil, "", time.Time{}, fmt.Errorf("generate OpenPGP keypair: %w", err)
	}
	var public bytes.Buffer
	if err := serializeArmored(&public, openpgp.PublicKeyType, entity.Serialize); err != nil {
		return nil, nil, "", time.Time{}, fmt.Errorf("serialize OpenPGP public key: %w", err)
	}
	var private bytes.Buffer
	if err := serializeArmored(&private, openpgp.PrivateKeyType, func(w io.Writer) error {
		return entity.SerializePrivate(w, config)
	}); err != nil {
		return nil, nil, "", time.Time{}, fmt.Errorf("serialize OpenPGP private key: %w", err)
	}
	fingerprint, err = PGPFingerprint(public.Bytes())
	if err != nil {
		return nil, nil, "", time.Time{}, err
	}
	return append([]byte(nil), public.Bytes()...), append([]byte(nil), private.Bytes()...), fingerprint, expires, nil
}

// PGPFingerprint returns the uppercase, 40-character v4 fingerprint for one
// ASCII-armored public key. Other key versions and multi-key bundles are not
// accepted.
func PGPFingerprint(publicArmor []byte) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicArmor))
	if err != nil || len(entities) != 1 || entities[0].PrimaryKey == nil ||
		entities[0].PrimaryKey.Version != 4 || len(entities[0].PrimaryKey.Fingerprint) != 20 {
		return "", ErrInvalidPGPKey
	}
	return strings.ToUpper(fmt.Sprintf("%X", entities[0].PrimaryKey.Fingerprint)), nil
}

// PGPPrivateFingerprint returns the v4 fingerprint of one unencrypted private-key block.
func PGPPrivateFingerprint(privateArmor []byte) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateArmor))
	if err != nil || len(entities) != 1 || entities[0].PrimaryKey == nil || entities[0].PrivateKey == nil || entities[0].PrivateKey.Encrypted || entities[0].PrivateKey.PrivateKey == nil ||
		entities[0].PrimaryKey.Version != 4 || len(entities[0].PrimaryKey.Fingerprint) != 20 {
		return "", ErrInvalidPGPKey
	}
	return strings.ToUpper(fmt.Sprintf("%X", entities[0].PrimaryKey.Fingerprint)), nil
}

// InspectPGPPrivateKey validates one unencrypted v4 private key and returns its
// public armor, fingerprint, identity addresses, and self-signature expiry.
func InspectPGPPrivateKey(privateArmor []byte) (publicArmor []byte, fingerprint string, identities []string, createdAt, expiresAt time.Time, err error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateArmor))
	if err != nil || len(entities) != 1 || entities[0].PrimaryKey == nil || entities[0].PrivateKey == nil || entities[0].PrivateKey.Encrypted || entities[0].PrivateKey.PrivateKey == nil ||
		entities[0].PrimaryKey.Version != 4 || len(entities[0].PrimaryKey.Fingerprint) != 20 {
		return nil, "", nil, time.Time{}, time.Time{}, ErrInvalidPGPKey
	}
	entity := entities[0]
	createdAt = entity.PrimaryKey.CreationTime.UTC()
	var lifetime *uint32
	for _, identity := range entity.Identities {
		if identity == nil || identity.UserId == nil {
			continue
		}
		identityText := strings.TrimSpace(identity.UserId.Id)
		if identityText == "" {
			identityText = strings.TrimSpace(identity.Name)
		}
		if identityText != "" {
			identities = append(identities, identityText)
		}
		if identity.SelfSignature != nil && identity.SelfSignature.KeyLifetimeSecs != nil && *identity.SelfSignature.KeyLifetimeSecs > 0 {
			value := *identity.SelfSignature.KeyLifetimeSecs
			if lifetime == nil || value < *lifetime {
				lifetime = &value
			}
		}
	}
	if createdAt.IsZero() || lifetime == nil {
		return nil, "", nil, time.Time{}, time.Time{}, fmt.Errorf("OpenPGP key has no valid expiry")
	}
	expiresAt = createdAt.Add(time.Duration(*lifetime) * time.Second)
	if !expiresAt.After(createdAt) {
		return nil, "", nil, time.Time{}, time.Time{}, fmt.Errorf("OpenPGP key expiry is invalid")
	}
	var public bytes.Buffer
	armoredWriter, err := armor.Encode(&public, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, "", nil, time.Time{}, time.Time{}, fmt.Errorf("armor imported public key: %w", err)
	}
	if err := entity.Serialize(armoredWriter); err != nil {
		_ = armoredWriter.Close()
		return nil, "", nil, time.Time{}, time.Time{}, fmt.Errorf("serialize imported public key: %w", err)
	}
	if err := armoredWriter.Close(); err != nil {
		return nil, "", nil, time.Time{}, time.Time{}, fmt.Errorf("finish imported public key: %w", err)
	}
	fingerprint, err = PGPFingerprint(public.Bytes())
	if err != nil {
		return nil, "", nil, time.Time{}, time.Time{}, err
	}
	sort.Strings(identities)
	return append([]byte(nil), public.Bytes()...), fingerprint, identities, createdAt, expiresAt, nil
}

// EncryptPGPMessage encrypts plaintext to one armored OpenPGP v4 public key.
func EncryptPGPMessage(publicArmor, plaintext []byte) ([]byte, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicArmor))
	if err != nil || len(entities) != 1 || entities[0].PrimaryKey == nil || entities[0].PrimaryKey.Version != 4 {
		return nil, ErrInvalidPGPKey
	}
	var armored bytes.Buffer
	writer, err := armor.Encode(&armored, openpgp.MessageType, nil)
	if err != nil {
		return nil, fmt.Errorf("armor encrypted OpenPGP message: %w", err)
	}
	message, err := openpgp.Encrypt(writer, entities, nil, nil, nil)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("initialize OpenPGP encryption: %w", err)
	}
	if _, err := message.Write(plaintext); err != nil {
		_ = message.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("write OpenPGP plaintext: %w", err)
	}
	if err := message.Close(); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("finish OpenPGP encryption: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish OpenPGP armor: %w", err)
	}
	return append([]byte(nil), armored.Bytes()...), nil
}

// DecryptPGPMessage decrypts one armored OpenPGP message with one private key.
func DecryptPGPMessage(privateArmor, encrypted []byte) ([]byte, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateArmor))
	if err != nil || len(entities) != 1 || entities[0].PrivateKey == nil || entities[0].PrivateKey.Encrypted || entities[0].PrimaryKey.Version != 4 {
		return nil, ErrInvalidPGPKey
	}
	block, err := armor.Decode(bytes.NewReader(encrypted))
	if err != nil || block.Type != openpgp.MessageType {
		return nil, fmt.Errorf("invalid armored OpenPGP message")
	}
	message, err := openpgp.ReadMessage(block.Body, openpgp.EntityList{entities[0]}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt OpenPGP message: %w", err)
	}
	plaintext, err := io.ReadAll(message.UnverifiedBody)
	if err != nil {
		return nil, fmt.Errorf("read OpenPGP plaintext: %w", err)
	}
	return plaintext, nil
}

// SignPGPPublicKey adds certifications from the old key to each identity on a new key.
func SignPGPPublicKey(newPublicArmor, oldPrivateArmor []byte) ([]byte, error) {
	newKeys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(newPublicArmor))
	if err != nil || len(newKeys) != 1 || newKeys[0].PrimaryKey == nil || newKeys[0].PrimaryKey.Version != 4 {
		return nil, ErrInvalidPGPKey
	}
	oldKeys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(oldPrivateArmor))
	if err != nil || len(oldKeys) != 1 || oldKeys[0].PrivateKey == nil || oldKeys[0].PrivateKey.Encrypted || oldKeys[0].PrimaryKey.Version != 4 {
		return nil, ErrInvalidPGPKey
	}
	newEntity := newKeys[0]
	oldEntity := oldKeys[0]
	identityIDs := make([]string, 0, len(newEntity.Identities))
	for identity := range newEntity.Identities {
		identityIDs = append(identityIDs, identity)
	}
	sort.Strings(identityIDs)
	for _, identity := range identityIDs {
		if err := newEntity.SignIdentity(identity, oldEntity, nil); err != nil {
			return nil, fmt.Errorf("certify rotated OpenPGP identity: %w", err)
		}
	}
	var output bytes.Buffer
	if err := serializeArmored(&output, openpgp.PublicKeyType, newEntity.Serialize); err != nil {
		return nil, fmt.Errorf("serialize signed OpenPGP public key: %w", err)
	}
	return append([]byte(nil), output.Bytes()...), nil
}

// EncryptPGPPrivateKey encrypts armored private-key bytes with AES-256-GCM.
// The AES key is derived from the 32-byte installation secret using HKDF-SHA256.
func EncryptPGPPrivateKey(privateArmor, installationSecret []byte) ([]byte, error) {
	if len(privateArmor) == 0 {
		return nil, fmt.Errorf("OpenPGP private key is empty")
	}
	aead, err := newPGPPrivateAEAD(installationSecret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate OpenPGP private-key nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, privateArmor, []byte(pgpPrivateCipherPrefix))
	encoded := make([]byte, len(pgpPrivateCipherPrefix)+base64.RawStdEncoding.EncodedLen(len(sealed)))
	copy(encoded, pgpPrivateCipherPrefix)
	base64.RawStdEncoding.Encode(encoded[len(pgpPrivateCipherPrefix):], sealed)
	return encoded, nil
}

// DecryptPGPPrivateKey decrypts an encrypted armored private key. It rejects
// unsupported encodings, malformed ciphertext, and authentication failures.
func DecryptPGPPrivateKey(encrypted, installationSecret []byte) ([]byte, error) {
	if !strings.HasPrefix(string(encrypted), pgpPrivateCipherPrefix) {
		return nil, fmt.Errorf("unsupported OpenPGP private-key ciphertext version")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(string(encrypted[len(pgpPrivateCipherPrefix):]))
	if err != nil {
		return nil, fmt.Errorf("decode OpenPGP private-key ciphertext")
	}
	aead, err := newPGPPrivateAEAD(installationSecret)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("invalid OpenPGP private-key ciphertext")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(pgpPrivateCipherPrefix))
	if err != nil {
		return nil, fmt.Errorf("decrypt OpenPGP private key")
	}
	return plaintext, nil
}

func newPGPPrivateAEAD(installationSecret []byte) (cipher.AEAD, error) {
	if len(installationSecret) != 32 {
		return nil, fmt.Errorf("installation secret must contain 32 bytes")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, installationSecret, []byte(pgpPrivateKeySalt), []byte(pgpPrivateKeyInfo)), key); err != nil {
		return nil, fmt.Errorf("derive OpenPGP private-key encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize OpenPGP private-key encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize OpenPGP private-key encryption: %w", err)
	}
	return aead, nil
}

func serializeArmored(dst io.Writer, keyType string, serialize func(io.Writer) error) error {
	writer, err := armor.Encode(dst, keyType, nil)
	if err != nil {
		return err
	}
	if err := serialize(writer); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
