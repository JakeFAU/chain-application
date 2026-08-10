package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/JakeFAU/chain-application/internal/config"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

func TestNewLoggerEmitsCloudLoggingJSON(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runtime.Logger().Warn("bounded message")

	entry := decodeLogEntry(t, &sink)
	if entry["severity"] != "WARNING" {
		t.Fatalf("severity = %v, want WARNING", entry["severity"])
	}
	if entry["message"] != "bounded message" {
		t.Fatalf("message = %v, want bounded message", entry["message"])
	}
	timestamp, ok := entry["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp = %T, want RFC3339-nano string", entry["timestamp"])
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		t.Fatalf("timestamp = %q, want RFC3339-nano: %v", timestamp, err)
	}
}

func TestNewLoggerEncodesBoundedLevels(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	cfg := loadTestConfig(t, map[string]string{"CHAIN_LOG_LEVEL": "debug"})
	runtime, err := New(t.Context(), cfg, WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		level    zapcore.Level
		severity string
	}{
		{level: zapcore.DebugLevel, severity: "DEBUG"},
		{level: zapcore.InfoLevel, severity: "INFO"},
		{level: zapcore.WarnLevel, severity: "WARNING"},
		{level: zapcore.ErrorLevel, severity: "ERROR"},
		{level: zapcore.DPanicLevel, severity: "CRITICAL"},
	}

	for _, test := range tests {
		runtime.Logger().Log(test.level, "bounded message")
	}

	decoder := json.NewDecoder(&sink)
	for _, test := range tests {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatalf("decode %s entry: %v", test.severity, err)
		}
		if entry["severity"] != test.severity {
			t.Fatalf("severity = %v, want %s", entry["severity"], test.severity)
		}
	}
}

func TestNewLoggerRejectsUnboundedLogLevel(t *testing.T) {
	t.Parallel()

	cfg := loadTestConfig(t, nil)
	cfg.LogLevel = config.LogLevel("critical")

	if _, err := New(t.Context(), cfg, WithLogSink(zapcore.AddSync(&bytes.Buffer{}))); err == nil {
		t.Fatal("New error = nil, want unsupported log level error")
	}
}

func TestTraceFieldsEmitsCloudLoggingCorrelation(t *testing.T) {
	t.Parallel()

	traceID := trace.TraceID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	spanID := trace.SpanID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	var sink bytes.Buffer
	runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runtime.Logger().Info("bounded message", TraceFields(ctx, "attribution-chain-505000")...)

	entry := decodeLogEntry(t, &sink)
	wantTrace := "projects/attribution-chain-505000/traces/" + traceID.String()
	if entry["logging.googleapis.com/trace"] != wantTrace {
		t.Fatalf("trace = %v, want %s", entry["logging.googleapis.com/trace"], wantTrace)
	}
	if entry["logging.googleapis.com/spanId"] != spanID.String() {
		t.Fatalf("span ID = %v, want %s", entry["logging.googleapis.com/spanId"], spanID)
	}
	if entry["logging.googleapis.com/trace_sampled"] != true {
		t.Fatalf("trace sampled = %v, want true", entry["logging.googleapis.com/trace_sampled"])
	}
}

func TestTraceFieldsRejectsInvalidCorrelation(t *testing.T) {
	t.Parallel()

	validSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01},
		SpanID:  trace.SpanID{0x01},
	})
	validContext := trace.ContextWithSpanContext(context.Background(), validSpanContext)

	tests := []struct {
		name      string
		ctx       context.Context
		projectID string
	}{
		{name: "invalid span context", ctx: context.Background(), projectID: "attribution-chain-505000"},
		{name: "empty project ID", ctx: validContext},
		{name: "malformed project ID", ctx: validContext, projectID: "not/a/project"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if fields := TraceFields(test.ctx, test.projectID); len(fields) != 0 {
				t.Fatalf("TraceFields returned %d fields, want none", len(fields))
			}
		})
	}
}

func decodeLogEntry(t *testing.T, sink *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any
	decoder := json.NewDecoder(sink)
	if err := decoder.Decode(&entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if err := decoder.Decode(&map[string]any{}); err != io.EOF {
		t.Fatalf("second log entry error = %v, want EOF", err)
	}
	return entry
}

func loadTestConfig(t *testing.T, values map[string]string) config.Config {
	t.Helper()

	cfg, err := config.Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}, "test-version")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}
