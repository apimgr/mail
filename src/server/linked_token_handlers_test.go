package server

import (
	"testing"
	"time"
)

func TestLinkedTokenExpiryRules(t *testing.T) {
	now := time.Date(2026, time.November, 26, 18, 30, 15, 0, time.FixedZone("offset", -5*60*60))
	today := "2026-11-26"
	got, err := linkedTokenExpiry(nil, &today, now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC).Unix()
	if !got.Valid || got.Int64 != want {
		t.Fatalf("today expires at %d, want %d", got.Int64, want)
	}
	never := "never"
	got, err = linkedTokenExpiry(&never, nil, now)
	if err != nil || got.Valid {
		t.Fatalf("never expiry = %+v, %v", got, err)
	}
	for _, value := range []string{"2026-11-25", "2026-02-29", "2026-11-26T00:00:00Z"} {
		if _, err := linkedTokenExpiry(nil, &value, now); err == nil {
			t.Errorf("accepted invalid or past expiry %q", value)
		}
	}
	week := "7d"
	got, err = linkedTokenExpiry(&week, nil, now)
	if err != nil || !got.Valid || got.Int64 != now.UTC().Add(7*24*time.Hour).Unix() {
		t.Fatalf("7d expiry = %+v, %v", got, err)
	}
	if _, err := linkedTokenExpiry(&never, &today, now); err == nil {
		t.Fatal("accepted both expires and expires_on")
	}
}
