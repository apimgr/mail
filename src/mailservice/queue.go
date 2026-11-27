package mailservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	ErrQueueUnavailable = errors.New("postfix queue is unavailable")
	ErrQueueNotFound    = errors.New("queue entry not found")
	ErrInvalidQueueID   = errors.New("invalid queue id")
	queueIDPattern      = regexp.MustCompile(`^[A-Za-z0-9]+$`)
)

const maxQueueOutput = 16 << 20

// QueueEntry is the metadata Postfix exposes for a queued message.
type QueueEntry struct {
	ID          string    `json:"id"`
	Queue       string    `json:"queue"`
	Size        int64     `json:"size_bytes"`
	Sender      string    `json:"sender"`
	Recipients  []string  `json:"recipients"`
	ArrivalTime time.Time `json:"arrival_time"`
	Reason      string    `json:"delivery_reason,omitempty"`
}

type postfixQueueRecord struct {
	QueueName  string `json:"queue_name"`
	QueueID    string `json:"queue_id"`
	Arrival    int64  `json:"arrival_time"`
	Size       int64  `json:"message_size"`
	Sender     string `json:"sender"`
	Recipients []struct {
		Address string `json:"address"`
		Reason  string `json:"delay_reason"`
	} `json:"recipients"`
}

// QueueManager provides bounded access to Postfix queue operations.
type QueueManager interface {
	List(context.Context) ([]QueueEntry, error)
	Get(context.Context, string) (QueueEntry, error)
	Operate(context.Context, string, string) error
}

type queueCommandRunner func(context.Context, string, ...string) ([]byte, error)

// PostfixQueue uses only fixed Postfix commands and never invokes a shell.
type PostfixQueue struct {
	run queueCommandRunner
}

func (queue PostfixQueue) List(ctx context.Context) ([]QueueEntry, error) {
	output, err := queue.runCommand(ctx, "postqueue", "-j")
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	return parseQueueOutput(output)
}

func parseQueueOutput(output []byte) ([]QueueEntry, error) {
	entries := make([]QueueEntry, 0)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var record postfixQueueRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, ErrQueueUnavailable
		}
		if !validQueueID(record.QueueID) || record.Arrival < 0 || record.Size < 0 {
			continue
		}
		entry := QueueEntry{ID: record.QueueID, Queue: safeQueueName(record.QueueName), Size: record.Size, Sender: record.Sender, Recipients: make([]string, 0, len(record.Recipients)), ArrivalTime: time.Unix(record.Arrival, 0).UTC()}
		for _, recipient := range record.Recipients {
			entry.Recipients = append(entry.Recipients, recipient.Address)
			if entry.Reason == "" && recipient.Reason != "" {
				entry.Reason = recipient.Reason
			}
		}
		entries = append(entries, entry)
	}
	if scanner.Err() != nil {
		return nil, ErrQueueUnavailable
	}
	return entries, nil
}

func (queue PostfixQueue) Get(ctx context.Context, id string) (QueueEntry, error) {
	if !validQueueID(id) {
		return QueueEntry{}, ErrInvalidQueueID
	}
	entries, err := queue.List(ctx)
	if err != nil {
		return QueueEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return QueueEntry{}, ErrQueueNotFound
}

func (queue PostfixQueue) Operate(ctx context.Context, id, action string) error {
	if action == "flush-all" {
		if _, err := queue.runCommand(ctx, "postqueue", "-f"); err != nil {
			return ErrQueueUnavailable
		}
		return nil
	}
	if !validQueueID(id) {
		return ErrInvalidQueueID
	}
	var command, option string
	switch action {
	case "flush":
		command, option = "postqueue", "-i"
	case "hold":
		command, option = "postsuper", "-h"
	case "release":
		command, option = "postsuper", "-H"
	case "delete":
		command, option = "postsuper", "-d"
	default:
		return fmt.Errorf("unsupported queue operation")
	}
	if _, err := queue.runCommand(ctx, command, option, id); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}

func (queue PostfixQueue) runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if queue.run != nil {
		return queue.run(ctx, name, args...)
	}
	return runBounded(ctx, name, args...)
}

func validQueueID(id string) bool {
	return id != "ALL" && queueIDPattern.MatchString(id)
}

func safeQueueName(value string) string {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "unknown"
	}
	return value
}

func runBounded(parent context.Context, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, ErrQueueUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	var output limitedBuffer
	output.limit = maxQueueOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil || output.overflow {
		return nil, ErrQueueUnavailable
	}
	return output.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	if len(value) > buffer.limit-buffer.Len() {
		buffer.overflow = true
		return 0, errors.New("output limit exceeded")
	}
	return buffer.Buffer.Write(value)
}
