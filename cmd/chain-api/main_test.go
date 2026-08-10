package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestNormalizeStandardStreamSyncError(t *testing.T) {
	otherFailure := errors.New("disk sync failed")
	regularFileFailure := &os.PathError{Op: "sync", Path: "/tmp/application.log", Err: syscall.EINVAL}
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil", err: nil, want: nil},
		{name: "invalid argument", err: &os.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EINVAL}, want: nil},
		{name: "not a terminal", err: &os.PathError{Op: "sync", Path: "/dev/stderr", Err: syscall.ENOTTY}, want: nil},
		{name: "standard stream descriptor unavailable", err: &os.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EBADF}, want: nil},
		{name: "regular file failure", err: regularFileFailure, want: regularFileFailure},
		{name: "other failure", err: otherFailure, want: otherFailure},
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
		})
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
