package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/mail/src/database"
)

type rateLimitBucket struct {
	count       int64
	previous    int64
	windowStart int64
	window      int64
}

type rateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]rateLimitBucket
	nextSweepAt int64
}

const maxRateLimitBuckets = 100000

func newRateLimiter(records []database.RateLimitRecord) *rateLimiter {
	limiter := &rateLimiter{buckets: make(map[string]rateLimitBucket, len(records))}
	for _, record := range records {
		window, err := strconv.ParseInt(record.Key[strings.LastIndex(record.Key, ":")+1:], 10, 64)
		if err != nil || window < 1 {
			continue
		}
		limiter.buckets[record.Key] = rateLimitBucket{count: record.Count, previous: record.Previous, windowStart: record.WindowStart, window: window}
	}
	return limiter
}

func (l *rateLimiter) allow(key string, limit, window int, now time.Time) (bool, int) {
	if limit < 1 || window < 1 {
		return false, window
	}
	key = fmt.Sprintf("%s:%d", key, window)
	nowUnix := now.UTC().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepExpired(nowUnix)
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxRateLimitBuckets {
			dropOldestRateLimitBucket(l.buckets)
		}
		bucket.windowStart = nowUnix
		bucket.window = int64(window)
	}
	elapsed := nowUnix - bucket.windowStart
	if elapsed < 0 {
		bucket.count = 0
		bucket.previous = 0
		bucket.windowStart = nowUnix
		elapsed = 0
	} else if elapsed >= int64(window) {
		if elapsed >= int64(window)*2 {
			bucket.previous = 0
		} else {
			bucket.previous = bucket.count
		}
		bucket.count = 0
		bucket.windowStart = nowUnix
		elapsed = 0
	}
	bucket.count++
	bucket.window = int64(window)
	l.buckets[key] = bucket
	effective := float64(bucket.count) + float64(bucket.previous)*(1-float64(elapsed)/float64(window))
	if effective <= float64(limit) {
		return true, 0
	}
	return false, rateLimitRetryAfter(bucket, int64(limit), int64(window), elapsed)
}

func (l *rateLimiter) isLimited(key string, limit, window int, now time.Time) (bool, int) {
	if limit < 1 || window < 1 {
		return true, window
	}
	key = fmt.Sprintf("%s:%d", key, window)
	nowUnix := now.UTC().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepExpired(nowUnix)
	bucket, ok := l.buckets[key]
	if !ok {
		return false, 0
	}
	elapsed := nowUnix - bucket.windowStart
	if elapsed < 0 {
		bucket.count = 0
		bucket.previous = 0
		bucket.windowStart = nowUnix
		elapsed = 0
	} else if elapsed >= int64(window) {
		if elapsed >= int64(window)*2 {
			bucket.previous = 0
		} else {
			bucket.previous = bucket.count
		}
		bucket.count = 0
		bucket.windowStart = nowUnix
		elapsed = 0
	}
	bucket.window = int64(window)
	l.buckets[key] = bucket
	effective := float64(bucket.count) + float64(bucket.previous)*(1-float64(elapsed)/float64(window))
	if effective < float64(limit) {
		return false, 0
	}
	return true, rateLimitRetryAfter(bucket, int64(limit), int64(window), elapsed)
}

func (l *rateLimiter) sweepExpired(nowUnix int64) {
	if nowUnix < l.nextSweepAt {
		return
	}
	for key, bucket := range l.buckets {
		if nowUnix-bucket.windowStart >= bucket.window*2 {
			delete(l.buckets, key)
		}
	}
	l.nextSweepAt = nowUnix + 60
}

func dropOldestRateLimitBucket(buckets map[string]rateLimitBucket) {
	var oldestKey string
	var oldestStart int64
	first := true
	for key, bucket := range buckets {
		if first || bucket.windowStart < oldestStart {
			oldestKey = key
			oldestStart = bucket.windowStart
			first = false
		}
	}
	if !first {
		delete(buckets, oldestKey)
	}
}

func rateLimitRetryAfter(bucket rateLimitBucket, limit, window, elapsed int64) int {
	if bucket.previous > 0 && bucket.count+1 <= limit {
		remainingPrevious := limit - bucket.count - 1
		targetElapsed := int64(math.Ceil(float64(window) * (1 - float64(remainingPrevious)/float64(bucket.previous))))
		wait := targetElapsed - elapsed
		if wait > 0 && wait < window-elapsed {
			return int(wait)
		}
	}
	wait := window - elapsed
	if bucket.count > 0 {
		extra := int64(math.Ceil(float64(window) * (1 - float64(limit-1)/float64(bucket.count))))
		if extra > 0 {
			wait += extra
		}
	}
	return max(1, int(wait))
}

func (l *rateLimiter) snapshot() []database.RateLimitRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	records := make([]database.RateLimitRecord, 0, len(l.buckets))
	for key, bucket := range l.buckets {
		records = append(records, database.RateLimitRecord{Key: key, Count: bucket.count, Previous: bucket.previous, WindowStart: bucket.windowStart})
	}
	return records
}

func (s *Server) enforceStrictRateLimit(w http.ResponseWriter, r *http.Request) bool {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if s.isTrustedProxy(r) {
		forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if forwarded != "" && net.ParseIP(forwarded) != nil {
			remote = forwarded
		}
	}
	limit := s.cfg.Server.RateLimit.Strict
	requests, window := limit.Requests, limit.Window
	if requests < 1 {
		requests = 10
	}
	if window < 1 {
		window = 900
	}
	allowed, retryAfter := s.limiter.allow("ip:"+remote+":strict", requests, window, time.Now())
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "RATE_LIMITED", "message": "Too many requests"})
	return false
}

func (s *Server) authFailureLocked(w http.ResponseWriter, r *http.Request) bool {
	if s.limiter == nil || s.cfg == nil {
		return false
	}
	limit := s.cfg.Server.RateLimit.AuthFailures
	requests, window := limit.Requests, limit.Window
	if requests < 1 {
		requests = 5
	}
	if window < 1 {
		window = 900
	}
	key := "ip:" + s.requestIP(r) + ":auth_failures"
	limited, retry := s.limiter.isLimited(key, requests, window, time.Now())
	if !limited {
		return false
	}
	writeRateLimitResponse(w, retry)
	return true
}

func (s *Server) recordAuthFailure(r *http.Request) {
	if s.limiter == nil || s.cfg == nil {
		return
	}
	limit := s.cfg.Server.RateLimit.AuthFailures
	requests, window := limit.Requests, limit.Window
	if requests < 1 {
		requests = 5
	}
	if window < 1 {
		window = 900
	}
	s.limiter.allow("ip:"+s.requestIP(r)+":auth_failures", requests, window, time.Now())
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil || s.cfg == nil {
			next.ServeHTTP(w, r)
			return
		}
		if s.authFailureLocked(w, r) {
			return
		}
		identity := s.rateLimitIdentity(r)
		if identity == "" {
			identity = "ip:" + s.requestIP(r)
		}
		burst := s.cfg.Server.RateLimit.GlobalBurst
		if burst < 1 {
			burst = 240
		}
		if allowed, retry := s.limiter.allow(identity+":global", burst, 60, time.Now()); !allowed {
			writeRateLimitResponse(w, retry)
			return
		}
		limit := s.cfg.Server.RateLimit.Read
		class := "read"
		if isHealthRateLimitedPath(r.URL.Path) {
			limit = s.cfg.Server.RateLimit.Health
			class = "health"
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
			limit = s.cfg.Server.RateLimit.Write
			class = "write"
		}
		if limit.Requests < 1 || limit.Window < 1 {
			switch class {
			case "read":
				limit.Requests, limit.Window = 120, 60
			case "health":
				limit.Requests, limit.Window = 120, 60
			default:
				limit.Requests, limit.Window = 10, 60
			}
		}
		if allowed, retry := s.limiter.allow(identity+":"+class, limit.Requests, limit.Window, time.Now()); !allowed {
			writeRateLimitResponse(w, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitIdentity(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "ip:" + s.requestIP(r)
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "ip:" + s.requestIP(r)
	}
	token := fields[1]
	digest := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(digest[:], s.operatorTokenHash[:]) == 1 {
		return "key:" + token[:min(12, len(token))]
	}
	if !validOwnerToken(token) || s.db == nil || s.db.ServerDB == nil {
		return "ip:" + s.requestIP(r)
	}
	identity, valid, err := s.db.ResolveOwnerToken(r.Context(), token[:12], hex.EncodeToString(digest[:]))
	if err != nil || !valid || identity.OwnerID == "" {
		return "ip:" + s.requestIP(r)
	}
	return "key:" + token[:12]
}

func isHealthRateLimitedPath(path string) bool {
	switch path {
	case "/healthz", "/server/healthz", "/api/healthz", "/api/healthz.txt", "/api/v1/server/healthz", "/api/v1/server/healthz.txt":
		return true
	default:
		return false
	}
}

func writeRateLimitResponse(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "RATE_LIMITED", "message": "Too many requests"})
}

func (s *Server) flushRateLimits() {
	if s.db == nil || s.db.ServerDB == nil || s.limiter == nil {
		return
	}
	if err := s.db.FlushRateLimits(context.Background(), s.limiter.snapshot(), time.Now()); err != nil {
		log.Printf("[WARN] Failed to flush rate-limit counters: %v", err)
	}
}
