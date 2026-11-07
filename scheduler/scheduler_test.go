package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestScheduleCalculationsAndTaskRegistration(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, schedule := range []string{"@every 2m", "@hourly", "@daily", "@weekly", "@monthly", "0 2 * * *", "@every invalid"} {
		if next := s.calculateNextRun(schedule); !next.After(time.Now().Add(-time.Second)) {
			t.Errorf("schedule %q returned a past next run: %v", schedule, next)
		}
	}
	s.registerBuiltInTasks()
	if len(s.tasks) != 7 {
		t.Fatalf("registered %d built-in tasks, want 7", len(s.tasks))
	}
	s.updateNextRun("missing")
	s.updateNextRun("session_cleanup")
	s.checkMissedTasks()
	if err := s.loadState(); err != nil {
		t.Fatal(err)
	}
	if err := s.saveState(); err != nil {
		t.Fatal(err)
	}
}
