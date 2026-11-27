package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestRequestStatsSnapshotAndPendingFlush(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	stats := newRequestStats(database.RequestStats{Total: 7, Hourly: map[int64]int64{now.Unix(): 2}})
	stats.begin()
	stats.record(now)
	snapshot := stats.snapshot(now)
	if snapshot.RequestsTotal != 8 || snapshot.Requests24h != 3 || snapshot.ActiveConns != 1 {
		t.Fatalf("request stats snapshot = %#v", snapshot)
	}
	stats.end()
	batch := stats.takePending()
	if batch.total != 1 || batch.hourly[now.Unix()] != 1 {
		t.Fatalf("pending request stats batch = %#v", batch)
	}
	stats.restore(batch)
	restored := stats.takePending()
	if restored.total != 1 || restored.hourly[now.Unix()] != 1 {
		t.Fatalf("restored request stats batch = %#v", restored)
	}
}

func TestServerHandlerCountsResponsesAndHealthShowsLiveRequests(t *testing.T) {
	server := New(&config.Config{}, nil)
	handler := server.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/server/about/", nil))
	if response.Code != http.StatusMovedPermanently {
		t.Fatalf("trailing slash status = %d, want %d", response.Code, http.StatusMovedPermanently)
	}
	if got := server.stats.snapshot(time.Now()).RequestsTotal; got != 1 {
		t.Fatalf("request total after redirect = %d, want 1", got)
	}
	response = httptest.NewRecorder()
	healthRequest := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
	healthRequest.Header.Set("Accept", "application/json")
	handler.ServeHTTP(response, healthRequest)
	var health healthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if health.Stats.RequestsTotal != 1 || health.Stats.ActiveConns != 1 {
		t.Fatalf("health stats = %#v, want total 1 and active 1", health.Stats)
	}
	if got := server.stats.snapshot(time.Now()).ActiveConns; got != 0 {
		t.Fatalf("active requests after response = %d, want 0", got)
	}
}

func TestShouldCountRequestExcludesHealthAndMetrics(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "/", want: true},
		{path: "/server/about", want: true},
		{path: "/server/healthz", want: false},
		{path: "/healthz", want: false},
		{path: "/api/healthz", want: false},
		{path: "/api/v1/server/healthz", want: false},
		{path: "/api/v1/server/healthz.txt", want: false},
		{path: "/server/metrics", want: false},
		{path: "/api/v1/server/metrics/prometheus", want: false},
		{path: "/metrics", want: false},
	}
	for _, test := range cases {
		t.Run(test.path, func(t *testing.T) {
			if got := shouldCountRequest(test.path); got != test.want {
				t.Fatalf("shouldCountRequest(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}
