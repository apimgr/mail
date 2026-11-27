package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/mail/src/common/api"
	"github.com/apimgr/mail/src/mailservice"
	"github.com/go-chi/chi/v5"
)

type mailQueueListResponse struct {
	OK         bool                     `json:"ok"`
	Data       []mailservice.QueueEntry `json:"data"`
	Pagination apiPagination            `json:"pagination"`
}

type mailQueueStatsResponse struct {
	Period          string         `json:"period"`
	Since           string         `json:"since"`
	QueuedMessages  int            `json:"queued_messages"`
	QueuedSizeBytes int64          `json:"queued_size_bytes"`
	ByState         map[string]int `json:"by_state"`
}

func (s *Server) handleMailQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	page, limit, valid := mailPagination(r)
	if !valid {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid pagination parameters"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	entries, err := s.mailQueue.List(ctx)
	if err != nil {
		writeQueueError(w, err)
		return
	}
	start := (page - 1) * limit
	end := start + limit
	if start > len(entries) {
		start = len(entries)
	}
	if end > len(entries) {
		end = len(entries)
	}
	pages := 0
	if len(entries) > 0 {
		pages = (len(entries)-1)/limit + 1
	}
	writeJSON(w, http.StatusOK, mailQueueListResponse{OK: true, Data: entries[start:end], Pagination: apiPagination{Page: page, Limit: limit, Total: len(entries), Pages: pages}})
}

func (s *Server) handleMailQueueStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	period, ok := readMailStatsPeriod(w, r)
	if !ok {
		return
	}
	since := time.Now().UTC().Add(-period.Duration)
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	entries, err := s.mailQueue.List(ctx)
	if err != nil {
		writeQueueError(w, err)
		return
	}
	counts := make(map[string]int)
	var size int64
	var total int
	for _, entry := range entries {
		if entry.ArrivalTime.Before(since) {
			continue
		}
		counts[entry.Queue]++
		size += entry.Size
		total++
	}
	writeJSON(w, http.StatusOK, apiDataResponse[mailQueueStatsResponse]{OK: true, Data: mailQueueStatsResponse{
		Period: period.Name, Since: since.Format(time.RFC3339), QueuedMessages: total, QueuedSizeBytes: size, ByState: counts,
	}})
}

func (s *Server) handleMailQueueEntry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	entry, err := s.mailQueue.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		writeQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": entry})
}

func (s *Server) handleMailQueueAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	action := strings.TrimPrefix(r.URL.Path, api.APIBasePath()+"/queue/")
	id := ""
	if action != "flush-all" {
		id, action, _ = strings.Cut(action, "/")
		if !strings.HasSuffix(r.URL.Path, "/"+action) {
			writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Queue operation not found"))
			return
		}
	}
	if action != "flush-all" && action != "flush" && action != "hold" && action != "release" {
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Queue operation not found"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := s.mailQueue.Operate(ctx, id, action); err != nil {
		s.auditRequest(r, "mail.queue."+action, "operator", map[string]any{"id": id, "success": false})
		writeQueueError(w, err)
		return
	}
	s.auditRequest(r, "mail.queue."+action, "operator", map[string]any{"id": id, "success": true})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "operation": action}})
}

func (s *Server) handleDeleteMailQueueEntry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := s.mailQueue.Operate(ctx, id, "delete"); err != nil {
		s.auditRequest(r, "mail.queue.delete", "operator", map[string]any{"id": id, "success": false})
		writeQueueError(w, err)
		return
	}
	s.auditRequest(r, "mail.queue.delete", "operator", map[string]any{"id": id, "success": true})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "operation": "delete"}})
}

func writeQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mailservice.ErrQueueNotFound):
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Queue entry not found"))
	case errors.Is(err, mailservice.ErrInvalidQueueID):
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid queue ID"))
	default:
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Postfix queue is unavailable"))
	}
}
