package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

const ambientSamplerHelperEnvironment = "CHAIN_TEST_AMBIENT_SAMPLER_HELPER"

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

func TestWriteFallbackTruncatesOnRuneBoundary(t *testing.T) {
	var output bytes.Buffer
	// U+20AC encodes as three bytes and the byte limit is not a multiple of
	// three, so a byte-slice cut at the limit lands inside a rune.
	errorText := strings.Repeat("\u20ac", maximumFallbackErrorBytes)

	writeFallback(&output, errors.New(errorText))

	written := output.String()
	if !utf8.ValidString(written) {
		t.Fatalf("writeFallback() wrote invalid UTF-8 ending in %q", written[max(0, len(written)-8):])
	}
	if len(written) > len(fallbackPrefix)+maximumFallbackErrorBytes+1 {
		t.Fatalf("writeFallback() wrote %d bytes, want at most %d", len(written), len(fallbackPrefix)+maximumFallbackErrorBytes+1)
	}
}

func TestBoundFallbackMessageBoundsWithoutSplittingRunes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		message      string
		wantMaximum  int
		wantValidUTF bool
	}{
		{name: "under limit", message: "short", wantMaximum: len("short"), wantValidUTF: true},
		{name: "at limit", message: strings.Repeat("x", maximumFallbackErrorBytes), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: true},
		{name: "two byte runes", message: strings.Repeat("\u00e9", maximumFallbackErrorBytes), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: true},
		{name: "three byte runes", message: strings.Repeat("\u20ac", maximumFallbackErrorBytes), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: true},
		{name: "four byte runes", message: strings.Repeat("\U0001d11e", maximumFallbackErrorBytes), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: true},
		{name: "mixed width runes", message: strings.Repeat("a\u20ac\u00e9\U0001d11e", maximumFallbackErrorBytes), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: true},
		// Already-invalid input must terminate and stay bounded; it is not repaired.
		{name: "already invalid bytes", message: strings.Repeat("\xff", maximumFallbackErrorBytes+10), wantMaximum: maximumFallbackErrorBytes, wantValidUTF: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			bounded := boundFallbackMessage(test.message)
			if len(bounded) > test.wantMaximum {
				t.Fatalf("boundFallbackMessage() length = %d, want at most %d", len(bounded), test.wantMaximum)
			}
			if test.wantValidUTF && !utf8.ValidString(bounded) {
				t.Fatalf("boundFallbackMessage() is not valid UTF-8, ending in %q", bounded[max(0, len(bounded)-8):])
			}
			if len(test.message) > maximumFallbackErrorBytes &&
				len(bounded) < maximumFallbackErrorBytes-(utf8.UTFMax-1) {
				t.Fatalf("boundFallbackMessage() stripped too much: length = %d", len(bounded))
			}
		})
	}
}

func TestRunRejectsAmbientOTELSamplerWithoutGlobalDiagnostic(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on occupied test port: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close occupied test port: %v", err)
		}
	})
	port := listener.Addr().(*net.TCPAddr).Port

	const privateSampler = "private-invalid-sampler-62fefba0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunAmbientSamplerHelper$")
	command.Env = []string{
		ambientSamplerHelperEnvironment + "=1",
		"PORT=" + strconv.Itoa(port),
		"CHAIN_OTEL_ENABLED=true",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317",
		"OTEL_TRACES_SAMPLER=" + privateSampler,
	}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("helper process did not exit within bound: %v", ctx.Err())
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != exitFailure {
		t.Fatalf("helper error = %v, output = %q; want exit code %d", err, output, exitFailure)
	}
	written := string(output)
	if strings.Contains(written, privateSampler) {
		t.Fatalf("process output exposed ambient sampler value: %q", written)
	}
	if strings.Contains(written, "unsupported sampler") {
		t.Fatalf("process output contains SDK global diagnostic: %q", written)
	}
	if !strings.Contains(written, "OTEL_TRACES_SAMPLER") {
		t.Fatalf("process output = %q, want bounded configuration variable", written)
	}
}

func TestRunAmbientSamplerHelper(t *testing.T) {
	if os.Getenv(ambientSamplerHelperEnvironment) != "1" {
		return
	}
	os.Exit(run())
}
