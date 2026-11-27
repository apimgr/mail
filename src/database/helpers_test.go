package database

import (
	"errors"
	"testing"
	"time"
)

func TestDatabaseErrorClassification(t *testing.T) {
	for _, message := range []string{"duplicate column name", "column already exists", "Duplicate column name: id"} {
		if !isColumnExistsError(errors.New(message)) {
			t.Errorf("column existence error not recognized: %q", message)
		}
	}
	if isColumnExistsError(nil) || isColumnExistsError(errors.New("unrelated")) {
		t.Fatal("unrelated error classified as duplicate column")
	}
	for _, message := range []string{"SQLSTATE 40001", "Error 1213: deadlock"} {
		if !isSerializationError(errors.New(message)) {
			t.Errorf("serialization error not recognized: %q", message)
		}
	}
	if isSerializationError(nil) || isSerializationError(errors.New("unrelated")) {
		t.Fatal("unrelated error classified as serialization failure")
	}
}

func TestTimeoutContext(t *testing.T) {
	ctx, cancel := timeoutContext(time.Second)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("timeout context has no deadline")
	}
}
