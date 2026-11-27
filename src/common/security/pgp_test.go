package security

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func TestGeneratePGPKey(t *testing.T) {
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("offset", -5*60*60))
	publicArmor, privateArmor, fingerprint, expires, err := GeneratePGPKey("Mail Security <security@example.net>", created)
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprint) != 40 || fingerprint != strings.ToUpper(fingerprint) {
		t.Fatalf("fingerprint = %q, want 40 uppercase hex characters", fingerprint)
	}
	wantExpiry := created.UTC().Truncate(time.Second).AddDate(2, 0, 0)
	if !expires.Equal(wantExpiry) {
		t.Fatalf("expires = %s, want %s", expires, wantExpiry)
	}
	if got, err := PGPFingerprint(publicArmor); err != nil || got != fingerprint {
		t.Fatalf("PGPFingerprint(public key) = %q, %v; want %q", got, err, fingerprint)
	}
	if got, err := PGPPrivateFingerprint(privateArmor); err != nil || got != fingerprint {
		t.Fatalf("PGPPrivateFingerprint(private key) = %q, %v; want %q", got, err, fingerprint)
	}
	inspectedPublic, inspectedFingerprint, identities, inspectedCreated, inspectedExpires, err := InspectPGPPrivateKey(privateArmor)
	inspectedPublicFingerprint, fingerprintErr := PGPFingerprint(inspectedPublic)
	if err != nil || fingerprintErr != nil || inspectedFingerprint != fingerprint || inspectedPublicFingerprint != fingerprint || !slices.Contains(identities, "Mail Security <security@example.net>") || !inspectedCreated.Equal(created.UTC().Truncate(time.Second)) || !inspectedExpires.Equal(expires) {
		t.Fatalf("InspectPGPPrivateKey() = %q, %v, %s, %s, error %v", inspectedFingerprint, identities, inspectedCreated, inspectedExpires, err)
	}
	if _, err := PGPPrivateFingerprint(publicArmor); err == nil {
		t.Fatal("PGPPrivateFingerprint(public key) error = nil")
	}
	if _, _, _, _, _, err := InspectPGPPrivateKey(publicArmor); err == nil {
		t.Fatal("InspectPGPPrivateKey(public key) error = nil")
	}
	publicBlock, err := armor.Decode(bytes.NewReader(publicArmor))
	if err != nil || publicBlock.Type != openpgp.PublicKeyType {
		t.Fatalf("public armor type = %q, error = %v", publicBlockType(publicBlock), err)
	}
	privateBlock, err := armor.Decode(bytes.NewReader(privateArmor))
	if err != nil || privateBlock.Type != openpgp.PrivateKeyType {
		t.Fatalf("private armor type = %q, error = %v", publicBlockType(privateBlock), err)
	}
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateArmor))
	if err != nil || len(entities) != 1 {
		t.Fatalf("read generated private key: entities=%d, error=%v", len(entities), err)
	}
	entity := entities[0]
	if entity.PrimaryKey.Version != 4 || entity.PrimaryKey.PubKeyAlgo != packet.PubKeyAlgoEdDSA {
		t.Fatalf("primary key version/algorithm = %d/%v, want v4 EdDSA", entity.PrimaryKey.Version, entity.PrimaryKey.PubKeyAlgo)
	}
	if len(entity.Subkeys) != 1 || entity.Subkeys[0].PublicKey.PubKeyAlgo != packet.PubKeyAlgoECDH {
		t.Fatalf("generated encryption subkeys = %#v, want one Curve25519 ECDH key", entity.Subkeys)
	}
}

func TestEncryptAndDecryptPGPPrivateKey(t *testing.T) {
	secret := bytes.Repeat([]byte{0x7a}, 32)
	privateArmor := []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nkey material\n-----END PGP PRIVATE KEY BLOCK-----\n")
	first, err := EncryptPGPPrivateKey(privateArmor, secret)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncryptPGPPrivateKey(privateArmor, secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("private-key encryption reused its nonce")
	}
	decrypted, err := DecryptPGPPrivateKey(first, secret)
	if err != nil || !bytes.Equal(decrypted, privateArmor) {
		t.Fatalf("private-key decryption = %q, %v", decrypted, err)
	}
	if _, err := DecryptPGPPrivateKey(first, bytes.Repeat([]byte{0x7b}, 32)); err == nil {
		t.Fatal("decryption accepted a different installation secret")
	}
	tampered := append([]byte(nil), first...)
	tampered[len(tampered)-1] ^= 1
	if _, err := DecryptPGPPrivateKey(tampered, secret); err == nil {
		t.Fatal("decryption accepted tampered ciphertext")
	}
	if _, err := EncryptPGPPrivateKey(privateArmor, secret[:31]); err == nil {
		t.Fatal("encryption accepted an invalid installation-secret length")
	}
	if _, err := DecryptPGPPrivateKey(first, secret[:31]); err == nil {
		t.Fatal("decryption accepted an invalid installation-secret length")
	}
	if _, err := EncryptPGPPrivateKey(nil, secret); err == nil {
		t.Fatal("encryption accepted an empty private key")
	}
	if _, err := DecryptPGPPrivateKey([]byte("unknown:v2"), secret); err == nil {
		t.Fatal("decryption accepted an unknown ciphertext version")
	}
}

func TestPGPFingerprintRejectsInvalidKeys(t *testing.T) {
	for _, value := range [][]byte{nil, []byte("not an armored key"), []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\n-----END PGP PUBLIC KEY BLOCK-----")} {
		if _, err := PGPFingerprint(value); err == nil {
			t.Errorf("PGPFingerprint(%q) unexpectedly succeeded", value)
		}
	}
}

func TestPGPMessageEncryptionAndRotationCertification(t *testing.T) {
	created := time.Now().UTC().Truncate(time.Second)
	oldPublic, oldPrivate, _, _, err := GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, newPrivate, newFingerprint, _, err := GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("confidential security report")
	encrypted, err := EncryptPGPMessage(oldPublic, plaintext)
	if err != nil {
		t.Fatalf("EncryptPGPMessage() error = %v", err)
	}
	decrypted, err := DecryptPGPMessage(oldPrivate, encrypted)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("DecryptPGPMessage() = %q, %v", decrypted, err)
	}
	if _, err := DecryptPGPMessage(newPrivate, encrypted); err == nil {
		t.Fatal("DecryptPGPMessage() accepted a different key")
	}
	if _, err := EncryptPGPMessage([]byte("invalid"), plaintext); err == nil {
		t.Fatal("EncryptPGPMessage() accepted an invalid public key")
	}
	if _, err := DecryptPGPMessage(oldPrivate, []byte("invalid")); err == nil {
		t.Fatal("DecryptPGPMessage() accepted an invalid message")
	}

	signedPublic, err := SignPGPPublicKey(newPublic, oldPrivate)
	if err != nil {
		t.Fatalf("SignPGPPublicKey() error = %v", err)
	}
	if got, err := PGPFingerprint(signedPublic); err != nil || got != newFingerprint {
		t.Fatalf("signed public fingerprint = %q, %v; want %q", got, err, newFingerprint)
	}
	signedMessage, err := EncryptPGPMessage(signedPublic, plaintext)
	if err != nil {
		t.Fatalf("encrypt to signed public key: %v", err)
	}
	if got, err := DecryptPGPMessage(newPrivate, signedMessage); err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypt message encrypted to rotated public key = %q, %v", got, err)
	}
	if _, err := SignPGPPublicKey([]byte("invalid"), oldPrivate); err == nil {
		t.Fatal("SignPGPPublicKey() accepted an invalid new public key")
	}
	if _, err := SignPGPPublicKey(newPublic, []byte("invalid")); err == nil {
		t.Fatal("SignPGPPublicKey() accepted an invalid old private key")
	}
}

func publicBlockType(block *armor.Block) string {
	if block == nil {
		return ""
	}
	return block.Type
}
