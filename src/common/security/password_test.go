package security

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerification(t *testing.T) {
	first, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "$argon2id$v=19$") {
		t.Fatalf("password verifier is not salted Argon2id")
	}
	if !VerifyPassword(first, "correct horse battery staple") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(first, "different password") {
		t.Fatal("incorrect password verified")
	}
	if VerifyPassword("$argon2id$v=19$m=4294967295,t=1,p=1$AA$AA", "correct horse battery staple") {
		t.Fatal("oversized attacker-controlled Argon2 cost was accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	for _, password := range []string{"short", " leading-space", "trailing-space ", strings.Repeat("x", 1025)} {
		if err := ValidatePassword(password); err == nil {
			t.Errorf("ValidatePassword(%q) succeeded", password)
		}
	}
	if err := ValidatePassword("valid-password"); err != nil {
		t.Fatalf("ValidatePassword() error = %v", err)
	}
}
