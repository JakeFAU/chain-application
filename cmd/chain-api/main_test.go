package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestNormalizeStandardStreamSyncError(t *testing.T) {
	otherFailure := errors.New("disk sync failed")
	standardOutputInvalid := &os.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EINVAL}
	standardErrorNotTerminal := &os.PathError{Op: "sync", Path: "/dev/stderr", Err: syscall.ENOTTY}
	standardOutputUnavailable := &os.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EBADF}
	regularFileFailure := &os.PathError{Op: "sync", Path: "/tmp/application.log", Err: syscall.EINVAL}
	nonSyncFailure := &os.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.EINVAL}
	tests := []struct {
		name    string
		err     error
		want    error
		notWant error
	}{
		{name: "nil", err: nil, want: nil},
		{name: "invalid argument", err: standardOutputInvalid, want: nil},
		{name: "not a terminal", err: standardErrorNotTerminal, want: nil},
		{name: "standard stream descriptor unavailable", err: standardOutputUnavailable, want: nil},
		{name: "regular file failure", err: regularFileFailure, want: regularFileFailure},
		{name: "other failure", err: otherFailure, want: otherFailure},
		{
			name:    "joined known and unrelated failures",
			err:     errors.Join(standardOutputInvalid, otherFailure),
			want:    otherFailure,
			notWant: standardOutputInvalid,
		},
		{
			name: "joined known failures only",
			err:  errors.Join(standardOutputInvalid, standardErrorNotTerminal),
			want: nil,
		},
		{name: "non-sync operation on standard stream", err: nonSyncFailure, want: nonSyncFailure},
		{name: "wrapped exact known failure", err: fmt.Errorf("flush logger: %w", standardOutputInvalid), want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := normalizeStandardStreamSyncError(test.err)
			if !errors.Is(err, test.want) {
				t.Fatalf("normalizeStandardStreamSyncError() = %v, want %v", err, test.want)
			}
			if test.want == nil && err != nil {
				t.Fatalf("normalizeStandardStreamSyncError() = %v, want nil", err)
			}
			if test.notWant != nil && errors.Is(err, test.notWant) {
				t.Fatalf("normalizeStandardStreamSyncError() = %v, unexpectedly retained %v", err, test.notWant)
			}
		})
	}
}

func TestNormalizeStandardStreamSyncErrorPreservesRemainingOrder(t *testing.T) {
	firstRemaining := errors.New("first remaining failure")
	secondRemaining := errors.New("second remaining failure")
	err := errors.Join(
		&os.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EINVAL},
		firstRemaining,
		&os.PathError{Op: "sync", Path: "/dev/stderr", Err: syscall.ENOTTY},
		secondRemaining,
	)

	filtered := normalizeStandardStreamSyncError(err)
	if filtered == nil {
		t.Fatal("normalizeStandardStreamSyncError() = nil, want remaining failures")
	}
	const expected = "first remaining failure\nsecond remaining failure"
	if filtered.Error() != expected {
		t.Fatalf("normalizeStandardStreamSyncError() = %q, want %q", filtered, expected)
	}
}

func TestWriteFallbackBoundsErrorOutput(t *testing.T) {
	var output bytes.Buffer
	errorText := strings.Repeat("x", maximumFallbackErrorBytes+1)

	writeFallback(&output, errors.New(errorText))

	written := output.String()
	if strings.Contains(written, errorText) {
		t.Fatal("writeFallback() wrote the unbounded error")
	}
	if len(written) > len(fallbackPrefix)+maximumFallbackErrorBytes+1 {
		t.Fatalf("writeFallback() wrote %d bytes, want at most %d", len(written), len(fallbackPrefix)+maximumFallbackErrorBytes+1)
	}
	if !strings.HasPrefix(written, fallbackPrefix) {
		t.Fatalf("writeFallback() output = %q, want prefix %q", written, fallbackPrefix)
	}
	if !strings.HasSuffix(written, "\n") {
		t.Fatalf("writeFallback() output = %q, want trailing newline", written)
	}
}
