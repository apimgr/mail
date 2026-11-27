package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestSchedulerRegistersRunsAndStopsTasks(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	s.RegisterTask("test", "Test", "@every 1h", func(context.Context) error {
		called = true
		return nil
	}, true)
	task := s.tasks["test"]
	if task == nil || task.NextRun.Before(time.Now()) {
		t.Fatal("registered task has no future run time")
	}
	s.runTask(task)
	if !called || task.RunCount != 1 || task.LastStatus != "success" {
		t.Fatalf("successful task state = %#v", task)
	}
	failure := errors.New("task failed")
	task.Handler = func(context.Context) error { return failure }
	s.runTask(task)
	if task.FailCount != 1 || task.LastStatus != "failed" || task.LastError != failure.Error() {
		t.Fatalf("failed task state = %#v", task)
	}
	s.Start()
	s.Stop()
}

func TestSetTaskHandlerOverridesBuiltInHandler(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := s.SetTaskHandler("update_check", func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.RegisterTask("update_check", "Update Check", "@every 1h", unavailableTask("test fallback"), true)
	s.runTask(s.tasks["update_check"])
	if !called {
		t.Fatal("configured handler did not replace the built-in fallback")
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	if err := s.SetTaskHandler("update_check", func(context.Context) error { return nil }); err == nil {
		t.Fatal("handler replacement after scheduler startup was accepted")
	}
}

func TestPGPKeyserverRetryHandlerConfiguration(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPGPKeyserverPublisher("", nil); err == nil {
		t.Fatal("empty configuration directory was accepted")
	}
	if err := s.SetPGPKeyserverPublisher(t.TempDir(), []string{"keys.openpgp.org"}); err != nil {
		t.Fatal(err)
	}
	s.RegisterTask("keyserver_retry", "PGP Keyserver Retry", "@every 15m", unavailableTask("test fallback"), true)
	if _, ok := s.tasks["keyserver_retry"]; !ok {
		t.Fatal("keyserver retry task was not registered")
	}
	if err := s.taskPGPKeyserverRetry(context.Background()); err == nil {
		t.Fatal("retry without a database succeeded")
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	if err := s.SetPGPKeyserverPublisher(t.TempDir(), nil); err == nil {
		t.Fatal("publisher replacement after scheduler startup was accepted")
	}
}

func TestSecurityCleanupTaskUsesConfiguredReportRetention(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "security-cleanup.db")
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
	now := time.Now().UTC()
	for _, id := range []string{"sec_aaaaaaaaaaaaaaaa", "sec_bbbbbbbbbbbbbbbb"} {
		if _, err := db.ServerDB.Exec(`INSERT INTO security_reports (
			tracking_id, id_window, severity, component, body_enc, body_alg, token_hash, created_at, updated_at
		) VALUES (?, 1, 'low', 'api', X'00', 'aes-256-gcm', ?, ?, ?)`, id, strings.Repeat("a", 64), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ServerDB.Exec(`UPDATE security_reports SET closed_at = ? WHERE tracking_id = ?`, now.Add(-48*time.Hour).Format(time.RFC3339Nano), "sec_aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecurityReportRetention(1); err != nil {
		t.Fatal(err)
	}
	s.registerBuiltInTasks()
	if err := s.tasks["security_cleanup"].Handler(context.Background()); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.ServerReadDB.QueryRow(`SELECT COUNT(*) FROM security_reports`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("remaining security reports = %d, %v; want the open report only", remaining, err)
	}
	if err := s.SetSecurityReportRetention(0); err == nil {
		t.Fatal("zero-day retention was accepted")
	}
}

func TestSecurityCleanupRemovesRetiredPGPKeyAfterThirtyDays(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "security-cleanup.db")
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
	now := time.Now().UTC().Truncate(time.Second)
	created := now.AddDate(0, -2, 0)
	rotated := now.Add(-31 * 24 * time.Hour)
	metadata := database.PGPKeyMetadata{
		Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567",
		CreatedAt:   created, ExpiresAt: created.AddDate(2, 0, 0), LastRotatedAt: &rotated,
		KeyserversPublished: []database.PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "config")
	securityDir := filepath.Join(configDir, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	retiredKey := filepath.Join(securityDir, "pgp.priv.old.asc.enc")
	if err := os.WriteFile(retiredKey, []byte("encrypted old key"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPGPKeyserverPublisher(configDir, []string{"keys.openpgp.org"}); err != nil {
		t.Fatal(err)
	}
	s.registerBuiltInTasks()
	if err := s.tasks["security_cleanup"].Handler(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(retiredKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired retired key remains: %v", err)
	}

	rotated = now
	metadata.LastRotatedAt = &rotated
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(retiredKey, []byte("encrypted current retired key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.tasks["security_cleanup"].Handler(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(retiredKey); err != nil {
		t.Fatalf("recently retired key was removed: %v", err)
	}
}

func TestSchedulerRecoversPanicsAndContinuesScheduling(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterTask("panic", "Panic", "@every 1h", func(context.Context) error {
		panic("broken task")
	}, true)
	panicking := s.tasks["panic"]
	s.runTask(panicking)
	if panicking.LastStatus != "failed" || panicking.FailCount != 1 || panicking.LastError != "task panic: broken task" {
		t.Fatalf("panic task state = %#v", panicking)
	}

	called := make(chan struct{}, 1)
	s.RegisterTask("healthy", "Healthy", "@every 1h", func(context.Context) error {
		called <- struct{}{}
		return nil
	}, true)
	s.mu.Lock()
	s.tasks["healthy"].NextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.checkAndRunTasks()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not dispatch a due task")
	}
	s.Stop()
}

func TestSchedulerRestoresAndCatchesUpPersistedTasks(t *testing.T) {
	cfg := &config.Config{}
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
	state := database.SchedulerTaskState{
		ID: "healthcheck_self", Name: "Self Health Check", Enabled: true,
		Schedule: "@every 5m", LastRun: time.Now().Add(-6 * time.Minute).Unix(),
		NextRun: time.Now().Add(time.Hour).Unix(), LastStatus: "running", RunCount: 4, FailCount: 2,
	}
	if err := db.SaveSchedulerTask(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{}, 1)
	s.RegisterTask(state.ID, state.Name, state.Schedule, func(context.Context) error {
		called <- struct{}{}
		return nil
	}, true)
	task := s.tasks[state.ID]
	if task.LastStatus != "interrupted" || task.RunCount != state.RunCount || task.FailCount != state.FailCount || task.NextRun.After(time.Now()) {
		t.Fatalf("restored task state = %#v", task)
	}
	s.checkMissedTasks()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("interrupted task was not caught up")
	}
	s.Stop()
}

func TestTokenCleanupRemovesExpiredCaptchaChallenges(t *testing.T) {
	cfg := &config.Config{}
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
	now := time.Now().UTC()
	for _, challenge := range []struct {
		id      string
		expires time.Time
	}{{"expired", now.Add(-time.Minute)}, {"valid", now.Add(time.Minute)}} {
		if _, err := db.ServerDB.Exec(`INSERT INTO captcha_challenges (id, answer_hash, expires_at) VALUES (?, ?, ?)`, challenge.id, "hash", challenge.expires); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.taskTokenCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.ServerDB.QueryRow(`SELECT COUNT(*) FROM captcha_challenges`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("remaining CAPTCHA challenges = %d, want 1", count)
	}
	var remaining string
	if err := db.ServerDB.QueryRow(`SELECT id FROM captcha_challenges`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != "valid" {
		t.Fatalf("remaining challenge = %q, want valid", remaining)
	}
}

func TestScheduleCalculationsAndTaskRegistration(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.location == nil || s.location.String() != "America/New_York" {
		t.Fatalf("scheduler timezone = %v, want America/New_York", s.location)
	}
	for _, schedule := range []string{"@every 2m", "@hourly", "@daily", "@weekly", "@monthly", "0 2 * * *", "@every invalid"} {
		if next := s.calculateNextRun(schedule); !next.After(time.Now().Add(-time.Second)) {
			t.Errorf("schedule %q returned a past next run: %v", schedule, next)
		}
	}
	s.registerBuiltInTasks()
	wantSchedules := map[string]string{
		"ssl_renewal": "0 3 * * *", "geoip_update": "0 3 * * 0",
		"blocklist_update": "0 4 * * *", "security_cleanup": "30 3 * * *",
		"keyserver_retry": "@every 15m", "cve_update": "0 5 * * *",
		"update_check": "0 6 * * *", "token_cleanup": "@every 15m",
		"log_rotation": "0 0 * * *", "backup_daily": "0 2 * * *",
		"backup_hourly": "@hourly", "disk_check": "@every 5m",
		"database_check": "@every 1m", "healthcheck_self": "@every 5m",
	}
	if len(s.tasks) != len(wantSchedules) {
		t.Fatalf("registered %d built-in tasks, want %d", len(s.tasks), len(wantSchedules))
	}
	for id, schedule := range wantSchedules {
		if task := s.tasks[id]; task == nil || task.Schedule != schedule {
			t.Errorf("built-in task %q = %#v, want schedule %q", id, task, schedule)
		}
	}
	if s.tasks["backup_hourly"].Enabled {
		t.Fatal("hourly backup must be disabled by default")
	}
	s.updateNextRun("missing")
	s.updateNextRun("token_cleanup")
	s.checkMissedTasks()
	if err := s.loadState(); err != nil {
		t.Fatal(err)
	}
	if err := s.saveState(); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerRetriesConfiguredFailuresWithExponentialBackoff(t *testing.T) {
	enabled := true
	s, err := New(nil, config.SchedulerConfig{Tasks: map[string]config.SchedulerTaskConfig{
		"blocklist_update": {Enabled: &enabled, RetryOnFail: true, RetryDelay: "1s"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.RegisterTask("blocklist_update", "Blocklist Update", "0 4 * * *", func(context.Context) error {
		return errors.New("provider unavailable")
	}, true)
	task := s.tasks["blocklist_update"]
	for attempt, expectedDelay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		before := time.Now()
		s.runTask(task)
		if task.RetryCount != attempt+1 || task.NextRun.Before(before.Add(expectedDelay)) {
			t.Fatalf("retry %d state = (count %d, next %s), expected delay %s", attempt+1, task.RetryCount, task.NextRun, expectedDelay)
		}
	}
	s.runTask(task)
	if task.RetryCount != 3 || task.LastStatus != "failed" || task.FailCount != 4 {
		t.Fatalf("exhausted retry state = %#v", task)
	}
	s.mu.Lock()
	task.Handler = func(context.Context) error { return nil }
	task.NextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.checkAndRunTasks()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		status := task.LastStatus
		s.mu.RUnlock()
		if status == "success" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s.mu.RLock()
	retryCount, status := task.RetryCount, task.LastStatus
	s.mu.RUnlock()
	if retryCount != 0 || status != "success" {
		t.Fatalf("successful retry reset state = %#v", task)
	}
}

func TestSchedulerUsesConfiguredTimezoneCatchUpAndTaskPolicies(t *testing.T) {
	backupEnabled := false
	healthEnabled := false
	settings := config.SchedulerConfig{
		Timezone:      "Asia/Tokyo",
		CatchUpWindow: "2h",
		Tasks: map[string]config.SchedulerTaskConfig{
			"backup_daily":     {Enabled: &backupEnabled, Schedule: "@every 30m"},
			"healthcheck_self": {Enabled: &healthEnabled, Schedule: "invalid"},
		},
	}
	s, err := New(nil, settings)
	if err != nil {
		t.Fatal(err)
	}
	if s.location.String() != "Asia/Tokyo" || s.catchUp != 2*time.Hour {
		t.Fatalf("scheduler settings = (%s, %s)", s.location, s.catchUp)
	}
	s.registerBuiltInTasks()
	if task := s.tasks["backup_daily"]; task.Enabled || task.Schedule != "@every 30m" {
		t.Fatalf("optional task policy = %#v", task)
	}
	if task := s.tasks["healthcheck_self"]; !task.Enabled || task.Schedule != "@every 5m" {
		t.Fatalf("mandatory task policy = %#v", task)
	}
	if task := s.tasks["database_check"]; !task.Enabled {
		t.Fatal("skippable database check should be enabled by default")
	}
}

func TestCronScheduleParsing(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, time.April, 1, 1, 30, 0, 0, location)
	tests := []struct {
		name string
		spec string
		want time.Time
	}{
		{name: "daily time", spec: "0 2 * * *", want: time.Date(2026, time.April, 1, 2, 0, 0, 0, location)},
		{name: "step list", spec: "*/15 * * * *", want: time.Date(2026, time.April, 1, 1, 45, 0, 0, location)},
		{name: "weekday alias", spec: "0 3 * * SUN", want: time.Date(2026, time.April, 5, 3, 0, 0, 0, location)},
		{name: "day of month", spec: "0 0 1 * *", want: time.Date(2026, time.May, 1, 0, 0, 0, 0, location)},
		{name: "day fields use cron OR semantics", spec: "0 0 1 * MON", want: time.Date(2026, time.April, 6, 0, 0, 0, 0, location)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextCronRun(test.spec, now)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("nextCronRun(%q) = %s, want %s", test.spec, got, test.want)
			}
		})
	}
	for _, spec := range []string{"", "0 0 * *", "61 * * * *", "*/0 * * * *", "1,,2 * * * *"} {
		if _, err := nextCronRun(spec, now); err == nil {
			t.Errorf("nextCronRun(%q) succeeded for invalid expression", spec)
		}
	}
}

func TestSpecialSchedulesUseCalendarBoundaries(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.March, 7, 12, 0, 0, 0, location)
	tests := []struct {
		schedule string
		want     time.Time
	}{
		{"@hourly", time.Date(2026, time.March, 7, 13, 0, 0, 0, location)},
		{"@daily", time.Date(2026, time.March, 8, 0, 0, 0, 0, location)},
		{"@weekly", time.Date(2026, time.March, 8, 0, 0, 0, 0, location)},
		{"@monthly", time.Date(2026, time.April, 1, 0, 0, 0, 0, location)},
	}
	for _, test := range tests {
		t.Run(test.schedule, func(t *testing.T) {
			got, ok := nextSpecialRun(test.schedule, now)
			if !ok || !got.Equal(test.want) {
				t.Fatalf("nextSpecialRun(%q) = %s (ok=%v), want %s", test.schedule, got, ok, test.want)
			}
		})
	}
}
