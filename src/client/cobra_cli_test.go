package main

import (
	"net/http"
	"testing"
)

func TestRequiresCLIConfirmationForDestructiveMailOperations(t *testing.T) {
	for _, test := range []struct {
		method string
		route  string
		want   bool
	}{
		{http.MethodDelete, "/users/{0}", true},
		{http.MethodPost, "/queue/{0}/flush", true},
		{http.MethodPost, "/queue/{0}/hold", true},
		{http.MethodPost, "/queue/{0}/release", true},
		{http.MethodPost, "/queue/flush-all", true},
		{http.MethodGet, "/queue", false},
		{http.MethodPost, "/users", false},
		{http.MethodPost, "/services/{0}/restart", false},
	} {
		if got := requiresCLIConfirmation(test.method, test.route); got != test.want {
			t.Errorf("requiresCLIConfirmation(%q, %q) = %t, want %t", test.method, test.route, got, test.want)
		}
	}
}
