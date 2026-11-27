package mailservice

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQueueIDValidationAndOperationMapping(t *testing.T) {
	for _, id := range []string{"ABC123", "a1B2c3"} {
		if !validQueueID(id) {
			t.Errorf("valid queue id %q rejected", id)
		}
	}
	for _, id := range []string{"", "ALL", "id;rm", "../id", "id-1", "id\nid"} {
		if validQueueID(id) {
			t.Errorf("unsafe queue id %q accepted", id)
		}
	}
	if err := (PostfixQueue{}).Operate(context.Background(), "id;touch", "delete"); !errors.Is(err, ErrInvalidQueueID) {
		t.Fatalf("unsafe operation id error = %v", err)
	}
	if err := (PostfixQueue{}).Operate(context.Background(), "id", "unknown"); err == nil {
		t.Fatal("unsupported action accepted")
	}
}

func TestParsePostfixQueueJSONLines(t *testing.T) {
	input := `{"queue_name":"deferred","queue_id":"ABC123","arrival_time":1700000000,"message_size":1024,"sender":"sender@example.test","recipients":[{"address":"to@example.test","delay_reason":"connect timed out"}]}` + "\n" +
		`{"queue_name":"hold","queue_id":"XYZ789","arrival_time":1700000001,"message_size":10,"sender":"","recipients":[]}`
	entries, err := parseQueueOutput([]byte(input))
	if err != nil || len(entries) != 2 {
		t.Fatalf("parsed entries = %#v, err=%v", entries, err)
	}
	if entries[0].Queue != "deferred" || entries[0].ID != "ABC123" || entries[0].Size != 1024 || entries[0].Reason != "connect timed out" || len(entries[0].Recipients) != 1 {
		t.Fatalf("first queue entry = %#v", entries[0])
	}
	if entries[0].ArrivalTime.Location() != time.UTC || entries[1].Queue != "hold" || entries[1].Recipients == nil {
		t.Fatalf("queue timestamps/state not normalized: %#v", entries)
	}
	if _, err := parseQueueOutput([]byte(strings.Repeat("x", 1<<20+1))); err == nil {
		t.Fatal("oversized JSON record accepted")
	}
	if _, err := parseQueueOutput([]byte("not-json")); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("malformed JSON error = %v", err)
	}
}

func TestLimitedBufferRejectsOversizedCommandOutput(t *testing.T) {
	buffer := limitedBuffer{limit: 3}
	if count, err := buffer.Write([]byte("abc")); err != nil || count != 3 {
		t.Fatalf("write within limit = %d, %v", count, err)
	}
	if _, err := buffer.Write([]byte("d")); err == nil || !buffer.overflow || !reflect.DeepEqual(buffer.Bytes(), []byte("abc")) {
		t.Fatalf("overflow write state = %q, %v, %v", buffer.Bytes(), buffer.overflow, err)
	}
}

func TestPostfixQueueUsesFixedCommandArguments(t *testing.T) {
	for _, test := range []struct {
		action  string
		command string
		args    []string
	}{
		{"flush", "postqueue", []string{"-i", "ABC123"}},
		{"hold", "postsuper", []string{"-h", "ABC123"}},
		{"release", "postsuper", []string{"-H", "ABC123"}},
		{"delete", "postsuper", []string{"-d", "ABC123"}},
		{"flush-all", "postqueue", []string{"-f"}},
	} {
		var gotCommand string
		var gotArgs []string
		queue := PostfixQueue{run: func(_ context.Context, command string, args ...string) ([]byte, error) {
			gotCommand, gotArgs = command, append([]string(nil), args...)
			return nil, nil
		}}
		id := "ABC123"
		if test.action == "flush-all" {
			id = ""
		}
		if err := queue.Operate(context.Background(), id, test.action); err != nil {
			t.Fatalf("Operate(%q) error = %v", test.action, err)
		}
		if gotCommand != test.command || !reflect.DeepEqual(gotArgs, test.args) {
			t.Errorf("Operate(%q) ran %q %v; want %q %v", test.action, gotCommand, gotArgs, test.command, test.args)
		}
	}
}

func TestPostfixQueueRejectsUnsafeIDsBeforeRunningCommand(t *testing.T) {
	called := false
	queue := PostfixQueue{run: func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}}
	for _, id := range []string{"ALL", "../ABC123", "abc;command"} {
		if err := queue.Operate(context.Background(), id, "delete"); !errors.Is(err, ErrInvalidQueueID) {
			t.Errorf("Operate(delete, %q) error = %v", id, err)
		}
	}
	if called {
		t.Fatal("unsafe queue ID reached the command runner")
	}
}

func TestPostfixQueueListRunsJSONMode(t *testing.T) {
	queue := PostfixQueue{run: func(_ context.Context, command string, args ...string) ([]byte, error) {
		if command != "postqueue" || !reflect.DeepEqual(args, []string{"-j"}) {
			t.Fatalf("List command = %q %v, want postqueue -j", command, args)
		}
		return []byte(`{"queue_name":"active","queue_id":"ABC123","arrival_time":1,"message_size":2,"sender":"","recipients":[]}`), nil
	}}
	entries, err := queue.List(context.Background())
	if err != nil || len(entries) != 1 || entries[0].ID != "ABC123" {
		t.Fatalf("List = %#v, %v", entries, err)
	}
}

func TestPostfixQueueErrorPathsAndInvalidRecords(t *testing.T) {
	failure := PostfixQueue{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("command failed")
	}}
	if _, err := failure.List(context.Background()); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("failed List() error = %v", err)
	}
	if _, err := failure.Get(context.Background(), "ABC123"); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("failed Get() error = %v", err)
	}
	if _, err := (PostfixQueue{}).Get(context.Background(), "bad-id"); !errors.Is(err, ErrInvalidQueueID) {
		t.Fatalf("invalid Get() ID error = %v", err)
	}
	queue := PostfixQueue{run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"queue_id":"ABC123","arrival_time":-1,"message_size":10}` + "\n" + `{"queue_id":"XYZ789","arrival_time":1,"message_size":-1}` + "\n" + `{"queue_id":"OK123","arrival_time":1,"message_size":1,"queue_name":"bad\nname"}`), nil
	}}
	entries, err := queue.List(context.Background())
	if err != nil || len(entries) != 1 || entries[0].Queue != "unknown" {
		t.Fatalf("invalid record filtering/sanitization = %#v, %v", entries, err)
	}
	if _, err := queue.Get(context.Background(), "MISSING"); !errors.Is(err, ErrQueueNotFound) {
		t.Fatalf("missing Get() error = %v", err)
	}
	if err := failure.Operate(context.Background(), "ABC123", "delete"); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("failed Operate() error = %v", err)
	}
	if err := failure.Operate(context.Background(), "", "flush-all"); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("failed flush-all error = %v", err)
	}
}

func TestRunBoundedRejectsMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := runBounded(context.Background(), "mail-command-that-does-not-exist"); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("runBounded() error = %v", err)
	}
}
