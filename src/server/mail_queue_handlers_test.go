package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/mailservice"
)

type fakeMailQueue struct {
	entries []mailservice.QueueEntry
	lastID  string
	lastOp  string
	err     error
}

func (queue *fakeMailQueue) List(context.Context) ([]mailservice.QueueEntry, error) {
	return queue.entries, queue.err
}

func (queue *fakeMailQueue) Get(_ context.Context, id string) (mailservice.QueueEntry, error) {
	if queue.err != nil {
		return mailservice.QueueEntry{}, queue.err
	}
	for _, entry := range queue.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return mailservice.QueueEntry{}, mailservice.ErrQueueNotFound
}

func (queue *fakeMailQueue) Operate(_ context.Context, id, action string) error {
	queue.lastID, queue.lastOp = id, action
	return queue.err
}

func TestMailQueueRoutesRequireOperatorAndListMetadata(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("a", 32)
	server := New(cfg, nil)
	server.mailQueue = &fakeMailQueue{entries: []mailservice.QueueEntry{{ID: "ABC123", Queue: "deferred", Size: 42, Sender: "sender@example.test", Recipients: []string{"to@example.test"}, ArrivalTime: time.Unix(100, 0).UTC()}}}
	handler := server.Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/queue", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized queue list status = %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/queue", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"ABC123"`) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("queue list response = %d %v %s", response.Code, response.Header(), response.Body.String())
	}
}

func TestMailQueueActionsUseFixedOperationNames(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("b", 32)
	queue := &fakeMailQueue{}
	server := New(cfg, nil)
	server.mailQueue = queue
	for _, test := range []struct {
		method, path, id, action string
	}{
		{http.MethodPost, "/api/v1/queue/ABC123/hold", "ABC123", "hold"},
		{http.MethodPost, "/api/v1/queue/ABC123/release", "ABC123", "release"},
		{http.MethodPost, "/api/v1/queue/ABC123/flush", "ABC123", "flush"},
		{http.MethodDelete, "/api/v1/queue/ABC123", "ABC123", "delete"},
		{http.MethodPost, "/api/v1/queue/flush-all", "", "flush-all"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || queue.lastID != test.id || queue.lastOp != test.action {
			t.Errorf("%s %s => %d (%q,%q), body=%s", test.method, test.path, response.Code, queue.lastID, queue.lastOp, response.Body.String())
		}
	}
}

func TestMailQueueUnavailableUsesServiceError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("c", 32)
	server := New(cfg, nil)
	server.mailQueue = &fakeMailQueue{err: errors.New("private command failure")}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/queue", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private command failure") {
		t.Fatalf("queue failure exposed internals: %d %s", response.Code, response.Body.String())
	}
}

func TestMailQueueStatsPeriodFiltersByArrival(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("d", 32)
	now := time.Now().UTC()
	server := New(cfg, nil)
	server.mailQueue = &fakeMailQueue{entries: []mailservice.QueueEntry{
		{ID: "RECENT", Queue: "deferred", Size: 42, ArrivalTime: now.Add(-time.Hour)},
		{ID: "OLD", Queue: "active", Size: 100, ArrivalTime: now.Add(-48 * time.Hour)},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/queue?period=24h", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{`"period":"24h"`, `"queued_messages":1`, `"queued_size_bytes":42`, `"by_state":{"deferred":1}`} {
		if response.Code != http.StatusOK || !strings.Contains(body, expected) {
			t.Fatalf("queue stats response = %d %s; missing %s", response.Code, body, expected)
		}
	}
}

func TestMailQueueStatsRejectsUnsupportedPeriod(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("e", 32)
	server := New(cfg, nil)
	server.mailQueue = &fakeMailQueue{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/queue?period=2y", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported queue period status = %d, body=%s", response.Code, response.Body.String())
	}
}
