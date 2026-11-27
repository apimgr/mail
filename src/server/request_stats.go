package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/mail/src/database"
)

type requestStats struct {
	mu            sync.Mutex
	total         int64
	pendingTotal  int64
	hourly        map[int64]int64
	pendingHourly map[int64]int64
	active        int64
}

type requestStatsBatch struct {
	total  int64
	hourly map[int64]int64
}

func newRequestStats(initial database.RequestStats) *requestStats {
	stats := &requestStats{
		total:         initial.Total,
		hourly:        make(map[int64]int64, len(initial.Hourly)),
		pendingHourly: make(map[int64]int64),
	}
	for hour, count := range initial.Hourly {
		stats.hourly[hour] = count
	}
	return stats
}

func (s *requestStats) begin() {
	s.mu.Lock()
	s.active++
	s.mu.Unlock()
}

func (s *requestStats) end() {
	s.mu.Lock()
	if s.active > 0 {
		s.active--
	}
	s.mu.Unlock()
}

func (s *requestStats) record(now time.Time) {
	hour := now.UTC().Truncate(time.Hour).Unix()
	s.mu.Lock()
	s.total++
	s.pendingTotal++
	s.hourly[hour]++
	s.pendingHourly[hour]++
	s.prune(now)
	s.mu.Unlock()
}

func (s *requestStats) snapshot(now time.Time) healthStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	cutoff := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour).Unix()
	var recent int64
	for hour, count := range s.hourly {
		if hour >= cutoff {
			recent += count
		}
	}
	return healthStats{RequestsTotal: s.total, Requests24h: recent, ActiveConns: s.active}
}

func (s *requestStats) takePending() requestStatsBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := requestStatsBatch{total: s.pendingTotal, hourly: s.pendingHourly}
	s.pendingTotal = 0
	s.pendingHourly = make(map[int64]int64)
	return batch
}

func (s *requestStats) restore(batch requestStatsBatch) {
	s.mu.Lock()
	s.pendingTotal += batch.total
	for hour, count := range batch.hourly {
		s.pendingHourly[hour] += count
	}
	s.mu.Unlock()
}

func (s *requestStats) prune(now time.Time) {
	cutoff := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour).Unix()
	for hour := range s.hourly {
		if hour < cutoff {
			delete(s.hourly, hour)
		}
	}
}

func shouldCountRequest(path string) bool {
	if path == "/server/healthz" || path == "/healthz" || path == "/api/healthz" ||
		strings.HasSuffix(path, "/server/healthz") || strings.HasSuffix(path, "/server/healthz.txt") {
		return false
	}
	if path == "/metrics" || path == "/server/metrics" || path == "/api/metrics" ||
		strings.HasPrefix(path, "/server/metrics/") || strings.HasPrefix(path, "/api/metrics/") ||
		strings.HasSuffix(path, "/server/metrics") || strings.Contains(path, "/server/metrics/") {
		return false
	}
	return true
}

func (s *Server) requestStatsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.stats.begin()
		defer s.stats.end()
		next.ServeHTTP(w, r)
		if shouldCountRequest(r.URL.Path) {
			s.stats.record(time.Now())
		}
	})
}

func (s *Server) flushRequestStats(ctx context.Context) {
	if s.db == nil || s.db.ServerDB == nil {
		return
	}
	batch := s.stats.takePending()
	if batch.total == 0 && len(batch.hourly) == 0 {
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.db.FlushRequestStats(flushCtx, batch.total, batch.hourly, time.Now()); err != nil {
		s.stats.restore(batch)
		log.Printf("[WARN] Failed to flush aggregate request stats: %v", err)
	}
}
