package server

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestOwnerTokenGenerationAndHash(t *testing.T) {
	token, err := generateOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validOwnerToken(token) {
		t.Fatalf("generated token has invalid format: %q", token[:4])
	}
	if hashOwnerToken(token) == hashOwnerToken("own_"+strings.Repeat("x", 32)) {
		t.Fatal("different owner tokens produced the same digest")
	}
	if !constantTimeHashEqual(hashOwnerToken(token), hashOwnerToken(token)) {
		t.Fatal("equal token hashes did not compare equal")
	}
	if constantTimeHashEqual(hashOwnerToken(token), "short") {
		t.Fatal("malformed token hash compared equal")
	}
}

func TestTokenViewCookieSignature(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, db)
	value, ok := s.signTokenViewCookie(t.Context(), "primary")
	if !ok {
		t.Fatal("could not sign a one-use token view cookie")
	}
	if card, valid := s.verifyTokenViewCookie(t.Context(), value); !valid || card != "primary" {
		t.Fatalf("signed token view cookie verified as %q, valid=%t", card, valid)
	}
	if _, valid := s.verifyTokenViewCookie(t.Context(), "linked-id."+strings.Repeat("A", 43)); valid {
		t.Fatal("tampered token view cookie signature was accepted")
	}
}

func TestOwnerTokenEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	token, err := generateOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := encryptOwnerToken(token, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, token) {
		t.Fatal("ciphertext contains the raw token")
	}
	decrypted, err := decryptOwnerToken(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt owner token: %v", err)
	}
	if decrypted != token {
		t.Fatalf("decrypted owner token did not match the generated token")
	}
	tampered := []byte(ciphertext)
	tampered[len(tampered)/2] ^= 1
	if _, err := decryptOwnerToken(string(tampered), key); err == nil {
		t.Fatal("modified ciphertext decrypted successfully")
	}
	if _, err := decryptOwnerToken(ciphertext, base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("z", 32)))); err == nil {
		t.Fatal("ciphertext decrypted with a different key")
	}
}

func TestSensitiveDataEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	plaintext := []byte("private DKIM material")
	ciphertext, err := encryptSensitiveData(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, string(plaintext)) || !strings.HasPrefix(ciphertext, "enc:v1:") {
		t.Fatalf("ciphertext did not use the versioned encrypted format: %q", ciphertext)
	}
	decrypted, err := decryptSensitiveData(ciphertext, key)
	if err != nil || string(decrypted) != string(plaintext) {
		t.Fatalf("decryptSensitiveData() = %q, %v", decrypted, err)
	}
	tampered := []byte(ciphertext)
	tampered[len("enc:v1:")+5] ^= 1
	if _, err := decryptSensitiveData(string(tampered), key); err == nil {
		t.Fatal("tampered sensitive-data ciphertext was accepted")
	}
	if _, err := decryptSensitiveData(ciphertext, base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))); err == nil {
		t.Fatal("sensitive-data ciphertext decrypted with a different key")
	}
}

func TestOwnerTokenValidationAndEncryptionKeyErrors(t *testing.T) {
	for _, token := range []string{"", "adm_" + strings.Repeat("a", 32), "own_" + strings.Repeat("!", 32), "own_" + strings.Repeat("a", 31)} {
		if validOwnerToken(token) {
			t.Fatalf("invalid token accepted: %q", token[:min(len(token), 4)])
		}
	}
	if _, err := encryptOwnerToken("not-an-owner-token", base64.RawStdEncoding.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("encryption accepted invalid owner-token input")
	}
	if _, err := decryptOwnerToken("invalid", "bad-key"); err == nil {
		t.Fatal("decryption accepted an invalid key")
	}
}
