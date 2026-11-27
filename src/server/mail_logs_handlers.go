package server

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

const maxMailLogLine = 1 << 20
const maxMailLogMessage = 200
const maxMailLogScanBytes = 64 << 20
const maxMailLogEntries = 100000

var (
	mailLogComponents = map[string]struct{}{
		"server": {}, "error": {}, "access": {}, "audit": {}, "security": {}, "debug": {},
	}
	mailLogCredentialField = regexp.MustCompile(`(?i)(["']?[a-z0-9_.-]*(?:token|secret|password|passwd|pwd|api[_-]?key|authorization|auth|credential|private[_-]?key|dsn|connection[_-]?string|database[_-]?url|db[_-]?url|database[_-]?user|db[_-]?user|username)[a-z0-9_.-]*["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}&]+)`)
	mailLogSensitiveQuery  = regexp.MustCompile(`(?i)([?&](?:token|session|code|key|password|secret|auth|pwd|api_key|apikey|access_token|refresh_token)=)[^&#\s]*`)
	mailLogURLUserInfo     = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s:]+(?::[^/@\s]*)?@`)
	mailLogInternalAddress = regexp.MustCompile(`(?i)\b(?:10\.(?:\d{1,3}\.){2}\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.(?:\d{1,3}\.)\d{1,3}|192\.168\.(?:\d{1,3}\.)\d{1,3}|127\.(?:\d{1,3}\.){2}\d{1,3}|169\.254\.(?:\d{1,3}\.)\d{1,3})\b`)
	mailLogFilesystemPath  = regexp.MustCompile(`(^|[\s"'=])(/(?:[A-Za-z0-9._-]+/)+[A-Za-z0-9._-]+|[A-Za-z]:\\[^\s"']+)`)
	mailLogISOTime         = regexp.MustCompile(`(?:time=)?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2}))`)
	mailLogApacheTime      = regexp.MustCompile(`\[(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})\]`)
)

type mailLogEntry struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	orderTime  time.Time
	filterText string
	fileRank   int
	lineIndex  int64
}

type mailLogPagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
	Pages int `json:"pages"`
}

type mailLogListResponse struct {
	OK         bool              `json:"ok"`
	Data       []mailLogEntry    `json:"data"`
	Pagination mailLogPagination `json:"pagination"`
}

func (s *Server) handleMailLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	component := chi.URLParam(r, "component")
	if _, ok := mailLogComponents[component]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid log component"})
		return
	}
	page, limit, ok := mailLogPage(r)
	queryValues := r.URL.Query()
	if !ok || len(queryValues["q"]) > 1 || len(queryValues["level"]) > 1 || len(queryValues.Get("q")) > 256 || len(queryValues.Get("level")) > 32 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid log filters or pagination"})
		return
	}
	if s.appPaths == nil || s.appPaths.Log == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail logs are unavailable"})
		return
	}
	levelFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	if levelFilter == "warning" {
		levelFilter = "warn"
	}
	query := strings.ToLower(r.URL.Query().Get("q"))
	entries, err := readMailLogEntries(s.appPaths.Log, component, levelFilter, query)
	if err != nil {
		if errors.Is(err, errMailLogLimit) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "RESULT_LIMIT_EXCEEDED", "message": "Log query exceeds the supported scan limit; narrow the filters"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail logs could not be loaded"})
		return
	}
	total := len(entries)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	data := append(make([]mailLogEntry, 0, end-start), entries[start:end]...)
	for i := range data {
		data[i].orderTime = time.Time{}
		data[i].filterText = ""
		data[i].fileRank = 0
		data[i].lineIndex = 0
	}
	pages := 0
	if total > 0 {
		pages = (total + limit - 1) / limit
	}
	writeJSON(w, http.StatusOK, mailLogListResponse{
		OK: true, Data: data,
		Pagination: mailLogPagination{Page: page, Limit: limit, Total: total, Pages: pages},
	})
}

func mailLogPage(r *http.Request) (int, int, bool) {
	page, limit := 1, 50
	for name, target := range map[string]*int{"page": &page, "limit": &limit} {
		values, exists := r.URL.Query()[name]
		if !exists {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			return 0, 0, false
		}
		value, err := strconv.Atoi(values[0])
		if err != nil || value < 1 {
			return 0, 0, false
		}
		*target = value
	}
	if limit > 100 || page > int(^uint(0)>>1)/limit {
		return 0, 0, false
	}
	return page, limit, true
}

var errMailLogLimit = errors.New("mail log scan limit exceeded")

func readMailLogEntries(logDir, component, levelFilter, query string) ([]mailLogEntry, error) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}
	baseName := component + ".log"
	type logFile struct {
		name    string
		modTime time.Time
		info    os.FileInfo
	}
	files := make([]logFile, 0)
	for _, entry := range entries {
		name := entry.Name()
		if name != baseName && !validMailRotatedLogName(baseName, name) {
			continue
		}
		info, err := os.Lstat(filepath.Join(logDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, logFile{name: name, modTime: info.ModTime(), info: info})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].name > files[j].name
		}
		return files[i].modTime.After(files[j].modTime)
	})
	var result []mailLogEntry
	var sequence int64
	var scanBytes int64
	for fileRank, candidate := range files {
		name := candidate.name
		if !strings.HasSuffix(name, ".gz") {
			if candidate.info.Size() > maxMailLogScanBytes-scanBytes {
				return nil, errMailLogLimit
			}
		}
		path := filepath.Join(logDir, name)
		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			return nil, errors.Join(err, file.Close())
		}
		if !info.Mode().IsRegular() || !os.SameFile(candidate.info, info) {
			if err := file.Close(); err != nil {
				return nil, err
			}
			continue
		}
		var reader io.Reader = file
		var compressed *gzip.Reader
		if strings.HasSuffix(name, ".gz") {
			compressed, err = gzip.NewReader(file)
			if err != nil {
				return nil, errors.Join(fmt.Errorf("open rotated log: %w", err), file.Close())
			}
			reader = compressed
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), maxMailLogLine)
		for scanner.Scan() {
			line := scanner.Text()
			scanBytes += int64(len(line) + 1)
			if scanBytes > maxMailLogScanBytes {
				var closeErr error
				if compressed != nil {
					closeErr = compressed.Close()
				}
				return nil, errors.Join(errMailLogLimit, closeErr, file.Close())
			}
			sequence++
			timestamp, level, parsed := parseMailLogMetadata(line)
			line = sanitizeMailLogLine(line)
			if levelFilter != "" && !strings.EqualFold(level, levelFilter) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(line), query) {
				continue
			}
			ordering := info.ModTime()
			if parsed {
				ordering = timestamp
			}
			result = append(result, mailLogEntry{Timestamp: timestampString(timestamp, parsed), Level: level, Message: truncateMailLogMessage(line), orderTime: ordering, filterText: line, fileRank: fileRank, lineIndex: sequence})
			if len(result) > maxMailLogEntries {
				var closeErr error
				if compressed != nil {
					closeErr = compressed.Close()
				}
				return nil, errors.Join(errMailLogLimit, closeErr, file.Close())
			}
		}
		scanErr := scanner.Err()
		var closeErr error
		if compressed != nil {
			closeErr = errors.Join(compressed.Close(), file.Close())
		} else {
			closeErr = file.Close()
		}
		if err := errors.Join(scanErr, closeErr); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].orderTime.Equal(result[j].orderTime) {
			if result[i].fileRank != result[j].fileRank {
				return result[i].fileRank < result[j].fileRank
			}
			return result[i].lineIndex > result[j].lineIndex
		}
		return result[i].orderTime.After(result[j].orderTime)
	})
	return result, nil
}

func validMailRotatedLogName(base, name string) bool {
	if !strings.HasPrefix(name, base+".") {
		return false
	}
	name = strings.TrimSuffix(name, ".gz")
	remainder := strings.TrimPrefix(name, base+".")
	parts := strings.Split(remainder, ".")
	if len(parts) < 1 || len(parts) > 2 || len(parts[0]) != len("2006-01-02") {
		return false
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		return false
	}
	if len(parts) == 2 {
		suffix, err := strconv.Atoi(parts[1])
		if err != nil || suffix < 1 {
			return false
		}
	}
	return true
}

func sanitizeMailLogLine(line string) string {
	line = mailLogCredentialField.ReplaceAllStringFunc(line, func(field string) string {
		parts := mailLogCredentialField.FindStringSubmatch(field)
		if len(parts) != 3 {
			return field
		}
		quote := ""
		if strings.HasPrefix(parts[2], `"`) || strings.HasPrefix(parts[2], `'`) {
			quote = parts[2][:1]
		}
		return parts[1] + quote + "[redacted]" + quote
	})
	line = mailLogURLUserInfo.ReplaceAllString(line, `${1}[redacted]@`)
	line = mailLogSensitiveQuery.ReplaceAllString(line, `${1}[redacted]`)
	line = mailLogInternalAddress.ReplaceAllString(line, "[redacted]")
	return mailLogFilesystemPath.ReplaceAllString(line, `${1}[redacted]`)
}

func truncateMailLogMessage(line string) string {
	if utf8.RuneCountInString(line) <= maxMailLogMessage {
		return line
	}
	runes := []rune(line)
	return string(runes[:maxMailLogMessage-3]) + "..."
}

func parseMailLogMetadata(line string) (time.Time, string, bool) {
	var fields map[string]any
	if json.Unmarshal([]byte(line), &fields) == nil {
		var timestamp time.Time
		for _, key := range []string{"time", "timestamp", "ts"} {
			if value, ok := fields[key].(string); ok {
				if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
					timestamp = parsed
					break
				}
			}
		}
		level, _ := fields["level"].(string)
		return timestamp, normalizeMailLogLevel(level), !timestamp.IsZero()
	}
	if matches := mailLogISOTime.FindStringSubmatch(line); len(matches) == 2 {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700"} {
			if parsed, err := time.Parse(layout, matches[1]); err == nil {
				return parsed, parseMailLogTextLevel(line), true
			}
		}
	}
	if matches := mailLogApacheTime.FindStringSubmatch(line); len(matches) == 2 {
		if parsed, err := time.Parse("02/Jan/2006:15:04:05 -0700", matches[1]); err == nil {
			return parsed, parseMailLogTextLevel(line), true
		}
	}
	return time.Time{}, parseMailLogTextLevel(line), false
}

func parseMailLogTextLevel(line string) string {
	lower := strings.ToLower(line)
	for _, level := range []string{"debug", "info", "warn", "warning", "error", "fatal", "critical"} {
		if strings.Contains(lower, "level="+level) || strings.Contains(lower, "["+level+"]") || strings.Contains(lower, " "+level+" ") {
			return normalizeMailLogLevel(level)
		}
	}
	return ""
}

func normalizeMailLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "warning":
		return "warn"
	case "debug", "info", "warn", "error", "fatal", "critical":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return ""
	}
}

func timestampString(value time.Time, recognized bool) string {
	if !recognized || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
