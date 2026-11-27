package api

import "testing"

func TestAPIBasePathUsesCanonicalVersion(t *testing.T) {
	if got, want := APIBasePath(), "/api/"+Version; got != want {
		t.Fatalf("APIBasePath() = %q, want %q", got, want)
	}
}
