package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const ownerTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func generateOwnerToken() (string, error) {
	const limit = byte(248)
	value := make([]byte, 0, 32)
	buffer := make([]byte, 64)
	for len(value) < 32 {
		if _, err := rand.Read(buffer); err != nil {
			return "", fmt.Errorf("generate owner token: %w", err)
		}
		for _, candidate := range buffer {
			if candidate < limit {
				value = append(value, ownerTokenAlphabet[int(candidate)%len(ownerTokenAlphabet)])
				if len(value) == 32 {
					break
				}
			}
		}
	}
	return "own_" + string(value), nil
}

func validOwnerToken(token string) bool {
	if len(token) != 36 || !strings.HasPrefix(token, "own_") {
		return false
	}
	for _, char := range token[4:] {
		if !strings.ContainsRune(ownerTokenAlphabet, char) {
			return false
		}
	}
	return true
}

func hashOwnerToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func constantTimeHashEqual(left, right string) bool {
	return len(left) == sha256.Size*2 && len(right) == sha256.Size*2 && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func encryptSensitiveData(plaintext []byte, encodedKey string) (string, error) {
	key, err := decodeEncryptionKey(encodedKey)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("initialize sensitive-data encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initialize sensitive-data encryption: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate sensitive-data encryption nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, plaintext, []byte("enc:v1"))
	return "enc:v1:" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func decryptSensitiveData(encoded, encodedKey string) ([]byte, error) {
	const prefix = "enc:v1:"
	if !strings.HasPrefix(encoded, prefix) {
		return nil, fmt.Errorf("unsupported sensitive-data ciphertext version")
	}
	key, err := decodeEncryptionKey(encodedKey)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		return nil, fmt.Errorf("decode sensitive-data ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize sensitive-data decryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize sensitive-data decryption: %w", err)
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("invalid sensitive-data ciphertext")
	}
	nonce, encrypted := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, encrypted, []byte("enc:v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt sensitive data")
	}
	return plaintext, nil
}

func encryptOwnerToken(token, encodedKey string) (string, error) {
	if !validOwnerToken(token) {
		return "", fmt.Errorf("invalid owner token")
	}
	key, err := decodeEncryptionKey(encodedKey)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("initialize owner-token encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initialize owner-token encryption: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate owner-token encryption nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(token), nil)
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func decryptOwnerToken(encoded, encodedKey string) (string, error) {
	key, err := decodeEncryptionKey(encodedKey)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode owner-token ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("initialize owner-token decryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initialize owner-token decryption: %w", err)
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return "", fmt.Errorf("invalid owner-token ciphertext")
	}
	nonce, encrypted := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt owner token")
	}
	token := string(plaintext)
	if !validOwnerToken(token) {
		return "", fmt.Errorf("decrypted owner token is invalid")
	}
	return token, nil
}

func decodeEncryptionKey(encoded string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("server encryption key must contain 32 bytes")
	}
	return key, nil
}
