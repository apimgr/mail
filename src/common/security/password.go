package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordHashMemory  = 64 * 1024
	passwordHashTime    = 3
	passwordHashThreads = 4
	passwordHashLength  = 32
	passwordSaltLength  = 16
)

// ValidatePassword applies the shared password rules before hashing.
func ValidatePassword(password string) error {
	if password != strings.TrimSpace(password) {
		return fmt.Errorf("password cannot start or end with whitespace")
	}
	if utf8.RuneCountInString(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 1024 {
		return fmt.Errorf("password exceeds the maximum length")
	}
	return nil
}

// HashPassword returns a PHC-encoded Argon2id password verifier.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, passwordHashTime, passwordHashMemory, passwordHashThreads, passwordHashLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		passwordHashMemory,
		passwordHashTime,
		passwordHashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a PHC-encoded Argon2id password verifier.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory uint32
	var iterations uint32
	var threads uint8
	for _, parameter := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(parameter, "=")
		if !ok {
			return false
		}
		switch key {
		case "m":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return false
			}
			memory = uint32(parsed)
		case "t":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return false
			}
			iterations = uint32(parsed)
		case "p":
			parsed, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return false
			}
			threads = uint8(parsed)
		default:
			return false
		}
	}
	if memory < 8*uint32(threads) || memory > 128*1024 || iterations == 0 || iterations > 5 || threads == 0 || threads > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < passwordSaltLength || len(salt) > 64 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(actual, want) == 1
}
