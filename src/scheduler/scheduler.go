package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
	"github.com/apimgr/mail/src/service"
)

// Scheduler manages background tasks per AI.md PART 18
type Scheduler struct {
	db     *database.DB
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	tasks                       map[string]*Task
	handlers                    map[string]TaskHandler
	loaded                      map[string]database.SchedulerTaskState
	location                    *time.Location
	config                      config.SchedulerConfig
	catchUp                     time.Duration
	securityReportRetentionDays int
	pgpSecurityDir              string
	pgpKeyservers               []string
	mu                          sync.RWMutex
	started                     bool
}

// Task represents a scheduled task
type Task struct {
	ID          string
	Name        string
	Schedule    string
	Handler     TaskHandler
	Enabled     bool
	RetryOnFail bool
	RetryDelay  time.Duration
	RetryCount  int
	LastRun     time.Time
	NextRun     time.Time
	RunCount    int64
	FailCount   int64
	LastStatus  string
	LastError   string
}

// TaskHandler is the function signature for task execution
type TaskHandler func(ctx context.Context) error

// New creates a new scheduler instance per AI.md PART 18
func New(db *database.DB, schedulerConfig ...config.SchedulerConfig) (*Scheduler, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.SchedulerConfig{Timezone: "America/New_York", CatchUpWindow: "1h"}
	if len(schedulerConfig) > 0 {
		cfg = schedulerConfig[0]
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "America/New_York"
	}
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Warnf("invalid server.scheduler.timezone %q, using default America/New_York", cfg.Timezone)
		cfg.Timezone = "America/New_York"
		location, err = time.LoadLocation(cfg.Timezone)
		if err != nil {
			logger.Warnf("default scheduler timezone America/New_York unavailable; using UTC")
			location = time.UTC
		}
	}
	catchUp, err := time.ParseDuration(cfg.CatchUpWindow)
	if err != nil || catchUp <= 0 {
		logger.Warnf("invalid server.scheduler.catch_up_window %q, using default 1h", cfg.CatchUpWindow)
		catchUp = time.Hour
	}

	s := &Scheduler{
		db:                          db,
		ctx:                         ctx,
		cancel:                      cancel,
		tasks:                       make(map[string]*Task),
		handlers:                    make(map[string]TaskHandler),
		loaded:                      make(map[string]database.SchedulerTaskState),
		location:                    location,
		config:                      cfg,
		catchUp:                     catchUp,
		securityReportRetentionDays: 365,
	}

	// Load task state from database
	if err := s.loadState(); err != nil {
		cancel()
		return nil, fmt.Errorf("loading scheduler state: %w", err)
	}

	return s, nil
}

// Start starts the scheduler per AI.md PART 18
func (s *Scheduler) Start() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	// Register built-in tasks per AI.md PART 19
	s.registerBuiltInTasks()
	if err := s.saveState(); err != nil {
		logger.Warnf("Failed to persist scheduler task definitions: %v", err)
	}

	// Check for missed tasks (catch-up)
	s.checkMissedTasks()

	// Start scheduler loop
	s.wg.Add(1)
	go s.run()

	s.mu.RLock()
	registered := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		registered = append(registered, id)
	}
	s.mu.RUnlock()
	sort.Strings(registered)
	logger.Infof("Scheduler started with tasks: %s", strings.Join(registered, ", "))
}

// Stop stops the scheduler gracefully per AI.md PART 19
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
	logger.Info("Scheduler stopping...")

	// Signal shutdown
	s.cancel()

	// Wait for running tasks (max 30 seconds per AI.md PART 19)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All scheduled tasks completed")
	case <-time.After(30 * time.Second):
		logger.Warn("Scheduler shutdown timeout - forcing stop")
		s.mu.Lock()
		for _, task := range s.tasks {
			if task.LastStatus == "running" {
				task.LastStatus = "interrupted"
			}
		}
		s.mu.Unlock()
	}

	// Save final state
	if err := s.saveState(); err != nil {
		logger.Errorf("Failed to save scheduler state: %v", err)
	}

	logger.Info("Scheduler stopped")
}

func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// TaskStates returns a stable snapshot of scheduler task state for diagnostics.
func (s *Scheduler) TaskStates() []database.SchedulerTaskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	states := make([]database.SchedulerTaskState, 0, len(s.tasks))
	for _, task := range s.tasks {
		states = append(states, schedulerState(task))
	}
	return states
}

// run is the main scheduler loop
func (s *Scheduler) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkAndRunTasks()
		}
	}
}

// checkAndRunTasks checks all tasks and runs those that are due
func (s *Scheduler) checkAndRunTasks() {
	now := time.Now()
	if s.location != nil {
		now = now.In(s.location)
	}
	due := make([]*Task, 0)
	s.mu.Lock()

	for _, task := range s.tasks {
		if !task.Enabled {
			continue
		}

		if !task.NextRun.After(now) {
			if task.RetryOnFail && task.RetryCount >= 3 {
				task.RetryCount = 0
			}
			due = append(due, task)
			task.NextRun = s.calculateNextRun(task.Schedule)
		}
	}
	s.mu.Unlock()

	for _, task := range due {
		if s.ctx.Err() != nil {
			return
		}
		s.wg.Add(1)
		go func(t *Task) {
			defer s.wg.Done()
			s.runTask(t)
		}(task)
	}
}

// runTask executes a single task
func (s *Scheduler) runTask(task *Task) {
	logger.Debugf("Running scheduled task: %s", task.Name)

	startTime := time.Now()

	// Execute task handler
	s.mu.Lock()
	task.LastRun = startTime
	task.LastStatus = "running"
	task.LastError = ""
	s.mu.Unlock()
	s.saveTaskState(task)

	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("task panic: %v", recovered)
			}
		}()
		err = task.Handler(s.ctx)
	}()

	duration := time.Since(startTime)

	// Update task status
	s.mu.Lock()
	if err != nil {
		task.LastStatus = "failed"
		task.LastError = err.Error()
		task.FailCount++
		if task.RetryOnFail && task.RetryCount < 3 {
			delay := task.RetryDelay * time.Duration(1<<task.RetryCount)
			task.RetryCount++
			task.NextRun = time.Now().Add(delay)
			logger.Warnf("Task %s failed: %v; retry %d/3 scheduled in %s (duration: %v)", task.Name, err, task.RetryCount, delay, duration)
		} else if task.RetryOnFail {
			logger.Errorf("Task %s failed after retries were exhausted: %v (duration: %v)", task.Name, err, duration)
		} else {
			logger.Warnf("Task %s failed: %v (duration: %v)", task.Name, err, duration)
		}
	} else {
		task.LastStatus = "success"
		task.LastError = ""
		task.RunCount++
		task.RetryCount = 0
		logger.Debugf("Task %s completed successfully (duration: %v)", task.Name, duration)
	}
	s.mu.Unlock()

	// Save task state to database
	s.saveTaskState(task)
	if s.db != nil && s.db.ServerDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.db.RecordSchedulerRun(ctx, task.ID, startTime, time.Now(), task.LastStatus, task.LastError, duration); err != nil {
			logger.Warnf("Failed to record scheduler history for %s: %v", task.ID, err)
		}
		cancel()
	}

	// Audit log per AI.md PART 11
	logger.Audit("task.executed", "system", map[string]interface{}{
		"task":     task.Name,
		"status":   task.LastStatus,
		"duration": duration.String(),
	})
}

// registerBuiltInTasks registers all required tasks per AI.md PART 18
func (s *Scheduler) registerBuiltInTasks() {
	s.RegisterTask("ssl_renewal", "SSL Certificate Renewal", "0 3 * * *", s.taskSSLRenewal, true)
	s.RegisterTask("geoip_update", "GeoIP Database Update", "0 3 * * 0", s.taskGeoIPUpdate, true)
	s.RegisterTask("blocklist_update", "Blocklist Update", "0 4 * * *", unavailableTask("blocklist update provider"), true)
	s.RegisterTask("security_cleanup", "Security Data Cleanup", "30 3 * * *", s.taskSecurityCleanup, true)
	s.RegisterTask("keyserver_retry", "PGP Keyserver Retry", "@every 15m", unavailableTask("PGP keyserver publishing"), true)
	s.RegisterTask("cve_update", "CVE Database Update", "0 5 * * *", unavailableTask("CVE and Trivy database update"), true)
	s.RegisterTask("update_check", "Release Update Check", "0 6 * * *", unavailableTask("release update check"), true)
	s.RegisterTask("token_cleanup", "Token Cleanup", "@every 15m", s.taskTokenCleanup, true)
	s.RegisterTask("log_rotation", "Log Rotation", "0 0 * * *", s.taskLogRotation, true)
	s.RegisterTask("backup_daily", "Daily Backup", "0 2 * * *", s.taskBackupDaily, true)
	s.RegisterTask("backup_hourly", "Hourly Backup", "@hourly", unavailableTask("hourly backup"), false)
	s.RegisterTask("disk_check", "Disk Space Check", "@every 5m", unavailableTask("disk space monitoring"), true)
	s.RegisterTask("database_check", "Database Connection Check", "@every 1m", s.taskDatabaseCheck, true)
	s.RegisterTask("healthcheck_self", "Self Health Check", "@every 5m", s.taskHealthCheck, true)
}

func unavailableTask(subsystem string) TaskHandler {
	return func(context.Context) error {
		return fmt.Errorf("%s is unavailable", subsystem)
	}
}

// SetSecurityReportRetention configures the closed-report retention before startup.
func (s *Scheduler) SetSecurityReportRetention(days int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("security report retention cannot change after scheduler startup")
	}
	if days < 1 {
		return fmt.Errorf("security report retention must be at least one day")
	}
	s.securityReportRetentionDays = days
	return nil
}

// SetPGPKeyserverPublisher configures the durable keyserver retry task.
func (s *Scheduler) SetPGPKeyserverPublisher(configDir string, hosts []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("PGP keyserver publisher cannot change after scheduler startup")
	}
	if strings.TrimSpace(configDir) == "" {
		return fmt.Errorf("configuration directory is required for PGP keyserver retries")
	}
	s.pgpSecurityDir = filepath.Join(configDir, "security")
	s.pgpKeyservers = append([]string(nil), hosts...)
	s.handlers["keyserver_retry"] = s.taskPGPKeyserverRetry
	return nil
}

// SetTaskHandler replaces a built-in task handler before scheduler startup.
func (s *Scheduler) SetTaskHandler(id string, handler TaskHandler) error {
	if id == "" || handler == nil {
		return fmt.Errorf("task ID and handler are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("cannot replace task handlers after scheduler startup")
	}
	s.handlers[id] = handler
	return nil
}

// RegisterTask registers a new task
func (s *Scheduler) RegisterTask(id, name, schedule string, handler TaskHandler, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if override, ok := s.handlers[id]; ok {
		handler = override
	}
	if configured, ok := s.config.Tasks[id]; ok {
		if configured.Schedule != "" {
			if validTaskSchedule(configured.Schedule) {
				schedule = configured.Schedule
			} else {
				logger.Warnf("invalid server.scheduler.tasks.%s.schedule %q, using default %q", id, configured.Schedule, schedule)
			}
		}
		if configured.Enabled != nil {
			if taskCanBeDisabled(id) {
				enabled = *configured.Enabled
			} else if !*configured.Enabled {
				logger.Warnf("server.scheduler.tasks.%s cannot be disabled; keeping it enabled", id)
			}
		}
	}
	retryOnFail := false
	retryDelay := 5 * time.Minute
	if configured, ok := s.config.Tasks[id]; ok {
		retryOnFail = configured.RetryOnFail
		if configured.RetryDelay != "" {
			if parsed, err := time.ParseDuration(configured.RetryDelay); err == nil && parsed > 0 {
				retryDelay = parsed
			} else {
				logger.Warnf("invalid server.scheduler.tasks.%s.retry_delay %q, using default 5m", id, configured.RetryDelay)
			}
		}
	}

	task := &Task{
		ID:          id,
		Name:        name,
		Schedule:    schedule,
		Handler:     handler,
		Enabled:     enabled,
		NextRun:     s.calculateNextRun(schedule),
		RetryOnFail: retryOnFail,
		RetryDelay:  retryDelay,
	}
	if saved, ok := s.loaded[id]; ok {
		task.LastRun = unixTime(saved.LastRun)
		task.RunCount = saved.RunCount
		task.FailCount = saved.FailCount
		task.LastStatus = saved.LastStatus
		task.LastError = saved.LastError
		if saved.Schedule == schedule && saved.NextRun > 0 {
			task.NextRun = time.Unix(saved.NextRun, 0)
		}
		if saved.LastStatus == "running" {
			task.LastStatus = "interrupted"
			task.NextRun = time.Now()
		}
		delete(s.loaded, id)
	}

	s.tasks[id] = task
}

func taskCanBeDisabled(id string) bool {
	switch id {
	case "geoip_update", "blocklist_update", "cve_update", "update_check", "backup_daily", "backup_hourly", "disk_check", "database_check":
		return true
	default:
		return false
	}
}

func validTaskSchedule(schedule string) bool {
	if strings.HasPrefix(schedule, "@every ") {
		duration, err := time.ParseDuration(strings.TrimPrefix(schedule, "@every "))
		return err == nil && duration > 0
	}
	if _, ok := nextSpecialRun(schedule, time.Now()); ok {
		return true
	}
	_, err := nextCronRun(schedule, time.Now())
	return err == nil
}

// calculateNextRun calculates the next run time based on schedule
// Per AI.md PART 19: Supports cron and interval formats
func (s *Scheduler) calculateNextRun(schedule string) time.Time {
	now := time.Now()
	if s.location != nil {
		now = now.In(s.location)
	}

	// Parse interval format (@every Xm, @every Xh)
	if len(schedule) > 7 && schedule[:7] == "@every " {
		duration, err := time.ParseDuration(schedule[7:])
		if err == nil {
			return now.Add(duration)
		}
	}

	if next, ok := nextSpecialRun(schedule, now); ok {
		return next
	}

	if next, err := nextCronRun(schedule, now); err == nil {
		return next
	}
	return now.Add(time.Hour)
}

func nextSpecialRun(schedule string, now time.Time) (time.Time, bool) {
	switch schedule {
	case "@hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location()), true
	case "@daily":
		return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location()), true
	case "@weekly":
		// Next Sunday at 00:00
		days := (7 - int(now.Weekday())) % 7
		if days == 0 {
			days = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day()+days, 0, 0, 0, 0, now.Location()), true
	case "@monthly":
		// First day of next month at 00:00
		year, month, _ := now.Date()
		return time.Date(year, month+1, 1, 0, 0, 0, 0, now.Location()), true
	}
	return time.Time{}, false
}

type cronField struct {
	values   map[int]struct{}
	wildcard bool
}

func nextCronRun(expression string, now time.Time) (time.Time, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have five fields")
	}
	months := map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
	weekdays := map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}
	minuteField, err := parseCronField(fields[0], 0, 59, nil, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron minute: %w", err)
	}
	hourField, err := parseCronField(fields[1], 0, 23, nil, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron hour: %w", err)
	}
	dayField, err := parseCronField(fields[2], 1, 31, nil, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron day: %w", err)
	}
	monthField, err := parseCronField(fields[3], 1, 12, months, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron month: %w", err)
	}
	weekdayField, err := parseCronField(fields[4], 0, 7, weekdays, true)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron weekday: %w", err)
	}

	location := now.Location()
	firstDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	limit := firstDay.AddDate(8, 0, 0)
	for day := firstDay; day.Before(limit); day = day.AddDate(0, 0, 1) {
		if _, ok := monthField.values[int(day.Month())]; !ok {
			continue
		}
		dayMatch := containsCron(dayField.values, day.Day())
		weekdayMatch := containsCron(weekdayField.values, int(day.Weekday()))
		if !dayField.wildcard && !weekdayField.wildcard {
			if !dayMatch && !weekdayMatch {
				continue
			}
		} else if !dayMatch || !weekdayMatch {
			continue
		}
		for hour := 0; hour < 24; hour++ {
			if _, ok := hourField.values[hour]; !ok {
				continue
			}
			for minute := 0; minute < 60; minute++ {
				if _, ok := minuteField.values[minute]; !ok {
					continue
				}
				candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, location)
				if candidate.After(now) {
					return candidate, nil
				}
			}
		}
	}
	return time.Time{}, fmt.Errorf("cron expression has no occurrence within eight years")
}

func parseCronField(value string, minimum, maximum int, aliases map[string]int, sundayAlias bool) (cronField, error) {
	parts := strings.Split(strings.ToUpper(value), ",")
	field := cronField{values: make(map[int]struct{}), wildcard: true}
	for _, part := range parts {
		if part == "" {
			return cronField{}, fmt.Errorf("empty list value")
		}
		step := 1
		base := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return cronField{}, fmt.Errorf("invalid step %q", part)
			}
			base = pieces[0]
			parsed, err := strconv.Atoi(pieces[1])
			if err != nil || parsed < 1 {
				return cronField{}, fmt.Errorf("invalid step %q", pieces[1])
			}
			step = parsed
		}
		if base != "*" && base != "?" {
			field.wildcard = false
		}
		start, end := minimum, maximum
		if base != "" && base != "*" && base != "?" {
			if strings.Contains(base, "-") {
				bounds := strings.Split(base, "-")
				if len(bounds) != 2 {
					return cronField{}, fmt.Errorf("invalid range %q", base)
				}
				var err error
				start, err = cronFieldValue(bounds[0], aliases)
				if err != nil {
					return cronField{}, err
				}
				end, err = cronFieldValue(bounds[1], aliases)
				if err != nil {
					return cronField{}, err
				}
			} else {
				var err error
				start, err = cronFieldValue(base, aliases)
				if err != nil {
					return cronField{}, err
				}
				if strings.Contains(part, "/") {
					end = maximum
				} else {
					end = start
				}
			}
		}
		if start < minimum || end > maximum || start > end {
			return cronField{}, fmt.Errorf("value %q outside %d..%d", part, minimum, maximum)
		}
		for current := start; current <= end; current += step {
			if sundayAlias && current == 7 {
				field.values[0] = struct{}{}
			} else {
				field.values[current] = struct{}{}
			}
		}
	}
	return field, nil
}

func cronFieldValue(value string, aliases map[string]int) (int, error) {
	if alias, ok := aliases[value]; ok {
		return alias, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid cron value %q", value)
	}
	return parsed, nil
}

func containsCron(values map[int]struct{}, value int) bool {
	_, ok := values[value]
	return ok
}

// updateNextRun updates the next run time for a task
func (s *Scheduler) updateNextRun(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task, ok := s.tasks[id]; ok {
		task.NextRun = s.calculateNextRun(task.Schedule)
	}
}

// checkMissedTasks checks for and runs missed tasks per AI.md PART 19
func (s *Scheduler) checkMissedTasks() {
	logger.Debug("Checking for missed tasks")
	now := time.Now()
	if s.location != nil {
		now = now.In(s.location)
	}
	s.mu.Lock()
	for _, task := range s.tasks {
		if !task.Enabled || task.NextRun.After(now) {
			continue
		}
		if now.Sub(task.NextRun) <= s.catchUp {
			task.NextRun = now
		} else {
			task.NextRun = s.calculateNextRun(task.Schedule)
		}
	}
	s.mu.Unlock()
	s.checkAndRunTasks()
}

// loadState loads scheduler state from database
func (s *Scheduler) loadState() error {
	if s.db == nil || s.db.ServerDB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	states, err := s.db.LoadSchedulerTasks(ctx)
	if err != nil {
		return err
	}
	s.loaded = states
	return nil
}

// saveState saves scheduler state to database
func (s *Scheduler) saveState() error {
	if s.db == nil || s.db.ServerDB == nil {
		return nil
	}
	s.mu.RLock()
	states := make([]database.SchedulerTaskState, 0, len(s.tasks))
	for _, task := range s.tasks {
		states = append(states, schedulerState(task))
	}
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, state := range states {
		if err := s.db.SaveSchedulerTask(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

// saveTaskState saves a single task's state to database
func (s *Scheduler) saveTaskState(task *Task) {
	if s.db == nil || s.db.ServerDB == nil {
		return
	}
	s.mu.RLock()
	state := schedulerState(task)
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.db.SaveSchedulerTask(ctx, state); err != nil {
		logger.Warnf("Failed to persist task state for %s: %v", state.ID, err)
	}
}

func schedulerState(task *Task) database.SchedulerTaskState {
	state := database.SchedulerTaskState{
		ID: task.ID, Name: task.Name, Enabled: task.Enabled, Schedule: task.Schedule,
		LastStatus: task.LastStatus, LastError: task.LastError,
		RunCount: task.RunCount, FailCount: task.FailCount,
	}
	if !task.LastRun.IsZero() {
		state.LastRun = task.LastRun.Unix()
	}
	if !task.NextRun.IsZero() {
		state.NextRun = task.NextRun.Unix()
	}
	return state
}

func unixTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}

// Built-in task handlers per AI.md PART 18.

func (s *Scheduler) taskTokenCleanup(ctx context.Context) error {
	if s.db == nil || s.db.ServerDB == nil {
		return fmt.Errorf("token cleanup requires the server database")
	}
	// Used challenges are deleted by their verifier; this removes any expired
	// challenges left behind when a caller never submits an answer.
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM captcha_challenges WHERE expires_at <= ?`, time.Now().UTC())
		return err
	}); err != nil {
		return fmt.Errorf("cleaning expired CAPTCHA challenges: %w", err)
	}
	return nil
}

func (s *Scheduler) taskSecurityCleanup(ctx context.Context) error {
	if s.db == nil || s.db.ServerDB == nil {
		return fmt.Errorf("security cleanup requires the server database")
	}
	now := time.Now().UTC()
	removed, err := s.db.DeleteClosedSecurityReports(ctx, s.securityReportRetentionDays, now)
	if err != nil {
		return err
	}
	if removed > 0 {
		logger.Infof("Removed %d expired closed security reports", removed)
	}
	metadata, err := s.db.GetPGPKeyMetadata(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read PGP rotation metadata for security cleanup: %w", err)
	}
	if err == nil && metadata.LastRotatedAt != nil && !now.Before(metadata.LastRotatedAt.Add(30*24*time.Hour)) && s.pgpSecurityDir != "" {
		oldPrivatePath := filepath.Join(s.pgpSecurityDir, "pgp.priv.old.asc.enc")
		if err := os.Remove(oldPrivatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove expired retired PGP private key: %w", err)
		}
	}
	return nil
}

func (s *Scheduler) taskPGPKeyserverRetry(ctx context.Context) error {
	if s.pgpSecurityDir == "" {
		return fmt.Errorf("PGP keyserver publisher is not configured")
	}
	return service.PublishPGPKeyservers(ctx, s.db, s.pgpSecurityDir, s.pgpKeyservers, time.Now().UTC(), true)
}

func (s *Scheduler) taskHealthCheck(ctx context.Context) error {
	if s.db == nil || s.db.ServerDB == nil || s.db.ServerReadDB == nil {
		return fmt.Errorf("self health check requires database read and write handles")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.db.ServerDB.PingContext(probeCtx); err != nil {
		return fmt.Errorf("self health write database probe: %w", err)
	}
	if err := s.db.ServerReadDB.PingContext(probeCtx); err != nil {
		return fmt.Errorf("self health read database probe: %w", err)
	}
	return nil
}

func (s *Scheduler) taskDatabaseCheck(ctx context.Context) error {
	return s.taskHealthCheck(ctx)
}

func (s *Scheduler) taskSSLRenewal(ctx context.Context) error {
	return fmt.Errorf("certificate renewal provider is unavailable")
}

func (s *Scheduler) taskGeoIPUpdate(ctx context.Context) error {
	return fmt.Errorf("GeoIP update provider is unavailable")
}

func (s *Scheduler) taskLogRotation(ctx context.Context) error {
	return fmt.Errorf("log rotation provider is unavailable")
}

func (s *Scheduler) taskBackupDaily(ctx context.Context) error {
	return fmt.Errorf("backup provider is unavailable")
}
