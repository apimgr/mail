package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestMailStatsRoutesRequireOperatorAndAggregatePeriod(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-operator-token"
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
	now := time.Now().UTC().Unix()
	for _, row := range []struct {
		status string
		stamp  int64
	}{
		{status: "delivered", stamp: now - int64(time.Hour.Seconds())},
		{status: "bounced", stamp: now - int64(48*time.Hour.Seconds())},
		{status: "old", stamp: now - int64((400 * 24 * time.Hour).Seconds())},
	} {
		if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_delivery_log
			(sender, recipient, status, timestamp) VALUES ('sender@example.test', 'recipient@example.test', ?, ?)`, row.status, row.stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		stamp int64
		spam  int
		virus int
	}{
		{stamp: now - int64(24*time.Hour.Seconds()), spam: 7, virus: 2},
		{stamp: now - int64((400 * 24 * time.Hour).Seconds()), spam: 11, virus: 5},
	} {
		if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_stats_daily
			(mail_server_id, day_timestamp, spam_count, virus_count) VALUES (1, ?, ?, ?)`, row.stamp, row.spam, row.virus); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_stats_hourly
		(mail_server_id, hour_timestamp, spam_count, virus_count) VALUES (1, ?, 3, 1)`, now-int64(time.Hour.Seconds())); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	requestID := 0
	newRequest := func(path, token string) *http.Request {
		requestID++
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2." + strconv.Itoa(requestID) + ":12345"
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		return request
	}
	for _, path := range []string{"/api/v1/stats/delivery?period=7d", "/api/v1/stats/spam?period=30d", "/api/v1/stats/spam?period=7d"} {
		request := newRequest(path, "")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s status = %d, want %d", path, response.Code, http.StatusUnauthorized)
		}
		request = newRequest(path, "own_"+strings.Repeat("a", 32))
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("owner-token %s status = %d, want %d", path, response.Code, http.StatusUnauthorized)
		}
		request = newRequest(path, cfg.Server.Token)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("operator %s status = %d: %s", path, response.Code, response.Body.String())
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data["period"] == nil || payload.Data["since"] == nil {
			t.Errorf("%s omitted period boundaries: %s", path, response.Body.String())
		}
		if path == "/api/v1/stats/delivery?period=7d" {
			if payload.Data["total"] != float64(2) {
				t.Errorf("delivery total = %v, want 2", payload.Data["total"])
			}
			statuses, ok := payload.Data["by_status"].(map[string]any)
			if !ok || statuses["delivered"] != float64(1) || statuses["bounced"] != float64(1) {
				t.Errorf("delivery statuses = %#v", payload.Data["by_status"])
			}
		} else if strings.HasSuffix(path, "period=30d") {
			if payload.Data["spam_count"] != float64(7) || payload.Data["virus_count"] != float64(2) || payload.Data["granularity"] != "day" {
				t.Errorf("daily spam stats = %#v, want counts 7/2", payload.Data)
			}
		} else if payload.Data["spam_count"] != float64(3) || payload.Data["virus_count"] != float64(1) || payload.Data["granularity"] != "hour" {
			t.Errorf("hourly spam stats = %#v, want counts 3/1", payload.Data)
		}
	}
	hourBoundary := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_stats_hourly
		(mail_server_id, hour_timestamp, spam_count, virus_count) VALUES (1, ?, 4, 2)`, hourBoundary.Unix()); err != nil {
		t.Fatal(err)
	}
	boundaryStats, err := db.GetMailSpamStats(context.Background(), hourBoundary.Add(time.Minute), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if boundaryStats.Spam != 7 || boundaryStats.Virus != 3 {
		t.Errorf("hourly stats at a partial period boundary = %d/%d, want whole first hour 7/3", boundaryStats.Spam, boundaryStats.Virus)
	}
	request := newRequest("/api/v1/stats/delivery?period=all", cfg.Server.Token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid period status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
