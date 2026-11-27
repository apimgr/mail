package logger

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level represents log severity levels
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// String returns the string representation of the level
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging per AI.md PART 11
type Logger struct {
	mu     sync.Mutex
	level  Level
	logDir string

	// Log files per AI.md PART 11
	serverLog   *os.File
	errorLog    *os.File
	accessLog   *os.File
	auditLog    *os.File
	securityLog *os.File
	debugLog    *os.File

	// Format settings
	format     string // "text" or "json"
	formats    map[string]string
	custom     map[string]string
	disabled   map[string]bool
	consoleOut io.Writer

	policies map[string]LogPolicy
}

// LogPolicy controls a log file's rotation, retention, and compression.
type LogPolicy struct {
	Rotate   string
	Keep     string
	Compress bool
}

// Config holds logger configuration
type Config struct {
	Level    string
	LogDir   string
	Format   string
	MaxSize  int64
	MaxAge   int
	Console  bool
	Policies map[string]LogPolicy
	Formats  map[string]string
	Custom   map[string]string
	Disabled map[string]bool
}

// Global logger instance
var (
	global *Logger
	once   sync.Once
)

// Init initializes the global logger per AI.md PART 8 Step 10
func Init(cfg Config) error {
	var err error
	once.Do(func() {
		global, err = New(cfg)
	})
	return err
}

// New creates a new logger instance
func New(cfg Config) (*Logger, error) {
	level := parseLevel(cfg.Level)
	console := io.Writer(io.Discard)
	if cfg.Console {
		console = os.Stdout
	}

	l := &Logger{
		level:      level,
		logDir:     cfg.LogDir,
		format:     cfg.Format,
		formats:    defaultFormats(cfg.Format, cfg.Formats),
		custom:     defaultCustom(cfg.Custom),
		disabled:   defaultDisabled(cfg.Disabled),
		policies:   defaultPolicies(cfg.Policies),
		consoleOut: console,
	}

	// Create log directory
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	for name, target := range l.fileTargets() {
		if l.disabled[name] {
			continue
		}
		opened, openErr := l.openLogFile(name + ".log")
		if openErr != nil {
			_ = l.Close()
			return nil, fmt.Errorf("opening %s.log: %w", name, openErr)
		}
		*target = opened
	}

	return l, nil
}

func defaultFormats(fallback string, overrides map[string]string) map[string]string {
	if fallback == "" {
		fallback = "text"
	}
	formats := map[string]string{"server": fallback, "error": fallback, "access": "apache", "audit": "json", "security": "fail2ban", "debug": fallback}
	for name, format := range overrides {
		if _, exists := formats[name]; exists && strings.TrimSpace(format) != "" {
			formats[name] = strings.ToLower(strings.TrimSpace(format))
		}
	}
	return formats
}

func defaultDisabled(overrides map[string]bool) map[string]bool {
	disabled := map[string]bool{"debug": true}
	for name, value := range overrides {
		switch name {
		case "server", "error", "access", "audit", "security", "debug":
			disabled[name] = value
		}
	}
	return disabled
}

func defaultCustom(overrides map[string]string) map[string]string {
	custom := make(map[string]string, len(overrides))
	for name, format := range overrides {
		if name == "access" && strings.TrimSpace(format) != "" {
			custom[name] = format
		}
	}
	return custom
}

func (l *Logger) fileTargets() map[string]**os.File {
	return map[string]**os.File{
		"server": &l.serverLog, "error": &l.errorLog, "access": &l.accessLog,
		"audit": &l.auditLog, "security": &l.securityLog, "debug": &l.debugLog,
	}
}

func (l *Logger) formatFor(kind string) string {
	if format := l.formats[kind]; format != "" {
		return format
	}
	return l.format
}

// openLogFile opens a log file for appending
func (l *Logger) openLogFile(name string) (*os.File, error) {
	path := filepath.Join(l.logDir, name)
	mode := os.FileMode(0o644)
	if name == "audit.log" || name == "security.log" {
		mode = 0o600
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func defaultPolicies(overrides map[string]LogPolicy) map[string]LogPolicy {
	policies := map[string]LogPolicy{
		"access":   {Rotate: "monthly", Keep: "none"},
		"audit":    {Rotate: "daily", Keep: "none"},
		"server":   {Rotate: "weekly,50MB", Keep: "none"},
		"error":    {Rotate: "weekly,50MB", Keep: "none"},
		"security": {Rotate: "weekly,50MB", Keep: "none"},
		"debug":    {Rotate: "weekly,50MB", Keep: "none"},
	}
	for name, policy := range overrides {
		if _, ok := policies[name]; !ok {
			continue
		}
		current := policies[name]
		if policy.Rotate != "" {
			current.Rotate = policy.Rotate
		}
		if policy.Keep != "" {
			current.Keep = policy.Keep
		}
		current.Compress = policy.Compress
		policies[name] = current
	}
	return policies
}

func (l *Logger) write(kind string, file **os.File, level Level, msg string, fields map[string]interface{}) {
	if *file == nil {
		return
	}
	if err := l.rotateIfNeeded(kind, file, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] log rotation failed for %s.log: %v\n", kind, err)
	}
	writeSlogEntry(*file, l.formatFor(kind), level, msg, fields)
}

func (l *Logger) rotateIfNeeded(kind string, file **os.File, now time.Time) error {
	info, err := (*file).Stat()
	if err != nil {
		return err
	}
	policy := l.policies[kind]
	if !rotationDue(policy.Rotate, info.ModTime().UTC(), now, info.Size()) {
		return nil
	}
	if err := (*file).Sync(); err != nil {
		return err
	}
	if err := (*file).Close(); err != nil {
		return err
	}
	path := filepath.Join(l.logDir, kind+".log")
	var rotateErr error
	if policy.Keep == "none" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			rotateErr = err
		}
	} else {
		rotated := uniqueRotationPath(path, now)
		if err := os.Rename(path, rotated); err != nil {
			rotateErr = err
		} else if policy.Compress {
			if err := compressFile(rotated); err != nil {
				rotateErr = err
			}
		}
	}
	opened, err := l.openLogFile(kind + ".log")
	if err != nil {
		return errors.Join(rotateErr, err)
	}
	*file = opened
	return errors.Join(rotateErr, pruneRotated(l.logDir, kind+".log", policy.Keep, now))
}

func rotationDue(policy string, modified, now time.Time, size int64) bool {
	policy = strings.TrimSpace(strings.ToLower(policy))
	if policy == "" || policy == "never" {
		return false
	}
	for _, trigger := range strings.Split(policy, ",") {
		trigger = strings.TrimSpace(trigger)
		switch trigger {
		case "daily":
			if modified.Format("2006-01-02") != now.Format("2006-01-02") {
				return true
			}
		case "weekly":
			if weekStart(modified) != weekStart(now) {
				return true
			}
		case "monthly":
			if modified.Year() != now.Year() || modified.Month() != now.Month() {
				return true
			}
		case "yearly":
			if modified.Year() != now.Year() {
				return true
			}
		default:
			if limit, ok := parseSize(trigger); ok && size >= limit {
				return true
			}
		}
	}
	return false
}

func weekStart(value time.Time) time.Time {
	day := (int(value.Weekday()) + 6) % 7
	return time.Date(value.Year(), value.Month(), value.Day()-day, 0, 0, 0, 0, time.UTC)
}

func parseSize(value string) (int64, bool) {
	upper := strings.ToUpper(strings.TrimSpace(value))
	var multiplier int64
	switch {
	case strings.HasSuffix(upper, "GB"):
		multiplier, upper = 1024*1024*1024, strings.TrimSuffix(upper, "GB")
	case strings.HasSuffix(upper, "MB"):
		multiplier, upper = 1024*1024, strings.TrimSuffix(upper, "MB")
	default:
		return 0, false
	}
	n, err := strconv.ParseInt(upper, 10, 64)
	return n * multiplier, err == nil && n > 0
}

func uniqueRotationPath(path string, now time.Time) string {
	base := path + "." + now.UTC().Format("2006-01-02")
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate += "." + strconv.Itoa(suffix)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func compressFile(path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	targetPath := path + ".gz"
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = source.Close()
		return err
	}
	writer := gzip.NewWriter(target)
	_, copyErr := io.Copy(writer, source)
	closeSourceErr := source.Close()
	closeWriterErr := writer.Close()
	closeTargetErr := target.Close()
	if err := errors.Join(copyErr, closeSourceErr, closeWriterErr, closeTargetErr); err != nil {
		_ = os.Remove(targetPath)
		return err
	}
	return os.Remove(path)
}

func pruneRotated(dir, current, keep string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	prefix := current + "."
	var files []os.FileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, info)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().After(files[j].ModTime()) })
	if keep == "forever" {
		return nil
	}
	if n, err := strconv.Atoi(keep); err == nil && n >= 0 {
		for _, file := range files[n:] {
			if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if days, ok := retentionDays(keep); ok {
		cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
		for _, file := range files {
			if file.ModTime().Before(cutoff) {
				if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func retentionDays(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "none" || value == "" {
		return 0, true
	}
	unit := value[len(value)-1]
	n, err := strconv.Atoi(value[:len(value)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch unit {
	case 'd':
		return n, true
	case 'w':
		return n * 7, true
	case 'm':
		return n * 365 / 12, true
	default:
		return 0, false
	}
}

// Close closes all log files
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error

	if l.serverLog != nil {
		if err := l.serverLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.serverLog = nil
	}
	if l.errorLog != nil {
		if err := l.errorLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.errorLog = nil
	}
	if l.accessLog != nil {
		if err := l.accessLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.accessLog = nil
	}
	if l.auditLog != nil {
		if err := l.auditLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.auditLog = nil
	}
	if l.securityLog != nil {
		if err := l.securityLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.securityLog = nil
	}
	if l.debugLog != nil {
		if err := l.debugLog.Close(); err != nil {
			errs = append(errs, err)
		}
		l.debugLog = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("closing log files: %v", errs)
	}

	return nil
}

// SetLevel changes the active threshold and opens or closes debug.log as needed.
func (l *Logger) SetLevel(value string) error {
	level, ok := parseConfiguredLevel(value)
	if !ok {
		return fmt.Errorf("invalid log level %q", value)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if level == DebugLevel && l.debugLog == nil && !l.disabled["debug"] {
		file, err := l.openLogFile("debug.log")
		if err != nil {
			return fmt.Errorf("opening debug.log: %w", err)
		}
		l.debugLog = file
	}
	if level != DebugLevel && l.debugLog != nil {
		if err := l.debugLog.Close(); err != nil {
			return fmt.Errorf("closing debug.log: %w", err)
		}
		l.debugLog = nil
	}
	l.level = level
	return nil
}

// Reopen safely switches every active log handle to the current path on disk.
// It is intended for external rotation tools that rename log files in place.
func (l *Logger) Reopen() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	for kind, target := range l.fileTargets() {
		if *target == nil {
			continue
		}
		opened, err := l.openLogFile(kind + ".log")
		if err != nil {
			errs = append(errs, fmt.Errorf("reopen %s.log: %w", kind, err))
			continue
		}
		old := *target
		*target = opened
		if err := old.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close old %s.log: %w", kind, err))
		}
	}
	return errors.Join(errs...)
}

// writeLog writes a log entry per AI.md PART 11
// Log files MUST use raw text only (no emojis, no ANSI codes)
func (l *Logger) writeLog(level Level, msg string, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Skip if below log level
	if level < l.level {
		return
	}

	// Write to appropriate log file
	var kind string
	var logFile **os.File
	switch level {
	case ErrorLevel:
		kind, logFile = "error", &l.errorLog
	case DebugLevel:
		kind, logFile = "debug", &l.debugLog
	default:
		kind, logFile = "server", &l.serverLog
	}
	l.write(kind, logFile, level, msg, fields)
	if kind != "server" {
		l.write("server", &l.serverLog, level, msg, fields)
	}
	if l.consoleOut != io.Discard {
		writeSlogEntry(l.consoleOut, l.formatFor(kind), level, msg, fields)
	}
}

func writeSlogEntry(file io.Writer, format string, level Level, msg string, fields map[string]interface{}) {
	if file == nil {
		return
	}
	options := &slog.HandlerOptions{Level: slogLevel(level)}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(file, options)
	} else {
		handler = slog.NewTextHandler(file, options)
	}
	attrs := make([]slog.Attr, 0, len(fields))
	for key, value := range fields {
		attrs = append(attrs, slog.Any(key, value))
	}
	slog.New(handler).LogAttrs(context.Background(), slogLevel(level), msg, attrs...)
}

func slogLevel(level Level) slog.Level {
	switch level {
	case DebugLevel:
		return slog.LevelDebug
	case WarnLevel:
		return slog.LevelWarn
	case ErrorLevel:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// formatJSON formats log entry as JSON per AI.md PART 11
func (l *Logger) formatJSON(timestamp string, level Level, msg string, fields map[string]interface{}) string {
	entry := make(map[string]interface{}, len(fields)+3)
	for key, value := range fields {
		entry[key] = value
	}
	entry["time"] = timestamp
	entry["level"] = level.String()
	entry["msg"] = msg
	return marshalLogEntry(entry)
}

func marshalLogEntry(entry map[string]interface{}) string {
	data, err := json.Marshal(entry)
	if err == nil {
		return string(data)
	}
	fallback, _ := json.Marshal(map[string]string{"level": "ERROR", "msg": "log entry could not be encoded"})
	return string(fallback)
}

// parseLevel converts string level to Level type
func parseLevel(s string) Level {
	level, ok := parseConfiguredLevel(s)
	if !ok {
		return InfoLevel
	}
	return level
}

func parseConfiguredLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return DebugLevel, true
	case "info":
		return InfoLevel, true
	case "warn", "warning":
		return WarnLevel, true
	case "error":
		return ErrorLevel, true
	default:
		return InfoLevel, false
	}
}

// Global logging functions

// Debug logs a debug message
func Debug(msg string) {
	if global != nil {
		global.writeLog(DebugLevel, msg, nil)
	}
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	if global != nil {
		global.writeLog(DebugLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Info logs an info message
func Info(msg string) {
	if global != nil {
		global.writeLog(InfoLevel, msg, nil)
	}
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	if global != nil {
		global.writeLog(InfoLevel, fmt.Sprintf(format, args...), nil)
	}
}

// InfoFields records an informational entry with structured fields.
func InfoFields(msg string, fields map[string]interface{}) {
	if global != nil {
		global.writeLog(InfoLevel, msg, fields)
	}
}

// Warn logs a warning message
func Warn(msg string) {
	if global != nil {
		global.writeLog(WarnLevel, msg, nil)
	}
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	if global != nil {
		global.writeLog(WarnLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Error logs an error message
func Error(msg string) {
	if global != nil {
		global.writeLog(ErrorLevel, msg, nil)
	}
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	if global != nil {
		global.writeLog(ErrorLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Close closes the global logger
func Close() error {
	if global != nil {
		return global.Close()
	}
	return nil
}

// SetLevel changes the process-wide log threshold after configuration loads.
func SetLevel(value string) error {
	if global == nil {
		return fmt.Errorf("logger is not initialized")
	}
	return global.SetLevel(value)
}

// Audit logs an audit event per AI.md PART 11
// Audit log MUST be JSON only (machine-parseable)
func Audit(event string, actor string, fields map[string]interface{}) {
	if global == nil || global.auditLog == nil {
		return
	}

	global.mu.Lock()
	defer global.mu.Unlock()

	timestamp := time.Now().Format(time.RFC3339)

	entry := make(map[string]interface{}, len(fields)+3)
	for key, value := range fields {
		entry[key] = value
	}
	entry["time"] = timestamp
	entry["event"] = event
	entry["actor"] = actor
	global.rotateSpecial("audit", &global.auditLog)
	_, _ = fmt.Fprintln(global.auditLog, marshalLogEntry(entry))
}

// Security logs a security event per AI.md PART 11
// Default format: fail2ban compatible
func Security(msg string, ip string) {
	if global == nil || global.securityLog == nil {
		return
	}

	global.mu.Lock()
	defer global.mu.Unlock()

	now := time.Now()
	timestamp := now.Format("2006-01-02 15:04:05")
	msg = strings.NewReplacer("\r", " ", "\n", " ").Replace(msg)
	ip = strings.NewReplacer("\r", " ", "\n", " ").Replace(ip)
	global.rotateSpecial("security", &global.securityLog)
	var entry string
	switch global.formatFor("security") {
	case "json":
		entry = marshalLogEntry(map[string]interface{}{"time": now.UTC().Format(time.RFC3339Nano), "level": "WARN", "category": "security", "msg": msg, "remote_ip": ip})
	case "syslog":
		entry = fmt.Sprintf("<132>%s %s mail-security: %s from %s", now.UTC().Format(time.RFC3339), os.Getenv("HOSTNAME"), msg, ip)
	case "cef":
		entry = fmt.Sprintf("CEF:0|Mail|mail|1|security|%s|5|src=%s", strings.NewReplacer("|", "\\|", "=", "\\=").Replace(msg), strings.NewReplacer(" ", "\\ ", "=", "\\=").Replace(ip))
	case "text":
		entry = fmt.Sprintf("%s [WARN] %s from %s", now.Format(time.RFC3339), msg, ip)
	default:
		entry = fmt.Sprintf("%s [security] %s from %s", timestamp, msg, ip)
	}
	_, _ = fmt.Fprintln(global.securityLog, entry)
	if global.consoleOut != io.Discard {
		_, _ = fmt.Fprintln(global.consoleOut, entry)
	}
}

// AccessRecord contains the request metadata available to access log formats.
type AccessRecord struct {
	IP         string
	Method     string
	Path       string
	Status     int
	Bytes      int
	UserAgent  string
	Referer    string
	Host       string
	Protocol   string
	TLSVersion string
	RequestID  string
	Country    string
	ASN        string
	Latency    time.Duration
	Time       time.Time
}

// Access logs an HTTP access event per AI.md PART 11.
func Access(ip, method, path string, status, size int, userAgent, referer string) {
	AccessRequest(AccessRecord{IP: ip, Method: method, Path: path, Status: status, Bytes: size, UserAgent: userAgent, Referer: referer})
}

// AccessRequest writes one sanitized request event using the configured access format.
func AccessRequest(record AccessRecord) {
	if global == nil || global.accessLog == nil {
		return
	}

	global.mu.Lock()
	defer global.mu.Unlock()

	now := record.Time
	if now.IsZero() {
		now = time.Now()
	}
	timestamp := now.Format("02/Jan/2006:15:04:05 -0700")
	if record.Referer == "" {
		record.Referer = "-"
	}
	if record.UserAgent == "" {
		record.UserAgent = "-"
	}
	for _, value := range []*string{&record.IP, &record.Method, &record.Path, &record.UserAgent, &record.Referer, &record.Host, &record.Protocol, &record.TLSVersion, &record.RequestID, &record.Country, &record.ASN} {
		*value = strings.NewReplacer("\r", " ", "\n", " ").Replace(*value)
	}

	var entry string
	switch global.formatFor("access") {
	case "json":
		entry = marshalLogEntry(map[string]interface{}{"time": now.UTC().Format(time.RFC3339Nano), "remote_ip": record.IP, "method": record.Method, "path": record.Path, "status": record.Status, "bytes": record.Bytes, "referer": record.Referer, "user_agent": record.UserAgent, "latency_ms": float64(record.Latency) / float64(time.Millisecond), "request_id": record.RequestID, "host": record.Host, "protocol": record.Protocol, "tls_version": record.TLSVersion, "country": record.Country, "asn": record.ASN})
	case "nginx":
		entry = fmt.Sprintf(`%s - - [%s] "%s %s %s" %d %d`, record.IP, timestamp, record.Method, record.Path, normalizedProtocol(record.Protocol), record.Status, record.Bytes)
	case "custom":
		entry = formatCustomAccess(global.custom["access"], record, now)
	case "apache", "":
		entry = fmt.Sprintf(`%s - - [%s] "%s %s %s" %d %d "%s" "%s"`, record.IP, timestamp, record.Method, record.Path, normalizedProtocol(record.Protocol), record.Status, record.Bytes, record.Referer, record.UserAgent)
	default:
		entry = fmt.Sprintf(`%s - - [%s] "%s %s %s" %d %d "%s" "%s"`, record.IP, timestamp, record.Method, record.Path, normalizedProtocol(record.Protocol), record.Status, record.Bytes, record.Referer, record.UserAgent)
	}

	global.rotateSpecial("access", &global.accessLog)
	_, _ = fmt.Fprintln(global.accessLog, entry)
	if global.consoleOut != io.Discard {
		_, _ = fmt.Fprintln(global.consoleOut, entry)
	}
}

func normalizedProtocol(protocol string) string {
	if protocol == "" {
		return "HTTP/1.1"
	}
	return protocol
}

func formatCustomAccess(format string, record AccessRecord, now time.Time) string {
	if format == "" {
		return ""
	}
	pathValue := record.Path
	query := ""
	if index := strings.IndexByte(pathValue, '?'); index >= 0 {
		query = pathValue[index+1:]
		pathValue = pathValue[:index]
	}
	values := map[string]string{
		"time": now.Format("15:04:05"), "date": now.Format("2006-01-02"), "datetime": now.Format(time.RFC3339),
		"remote_ip": record.IP, "method": record.Method, "path": pathValue, "query": query,
		"status": strconv.Itoa(record.Status), "bytes": strconv.Itoa(record.Bytes),
		"latency": record.Latency.String(), "latency_ms": strconv.FormatFloat(float64(record.Latency)/float64(time.Millisecond), 'f', 3, 64),
		"user_agent": record.UserAgent, "referer": record.Referer, "request_id": record.RequestID,
		"fqdn": record.Host, "protocol": record.Protocol, "tls_version": record.TLSVersion,
		"country": record.Country, "asn": record.ASN,
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	replacements := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		replacements = append(replacements, "{"+key+"}", values[key])
	}
	return strings.NewReplacer(replacements...).Replace(format)
}

func (l *Logger) rotateSpecial(kind string, file **os.File) {
	if err := l.rotateIfNeeded(kind, file, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] log rotation failed for %s.log: %v\n", kind, err)
	}
}

// Reopen reopens the active process-wide log files.
func Reopen() error {
	if global == nil {
		return errors.New("logger is not initialized")
	}
	return global.Reopen()
}
