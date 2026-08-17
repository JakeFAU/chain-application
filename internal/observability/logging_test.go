package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JakeFAU/chain-application/internal/config"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

const (
	sampledCloudTraceID      = "0123456789abcdef0123456789abcdef"
	sampledCloudTraceContext = sampledCloudTraceID + "/81985529216486895;o=1"
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

func TestWrapHTTPLogsBoundedRequestWithTraceCorrelation(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	cfg := loadTestConfig(t, map[string]string{"CHAIN_GCP_PROJECT_ID": "attribution-chain-505000"})
	runtime, err := New(t.Context(), cfg, WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runtime.tracerProvider = newRecordingTracerProvider(t)

	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	request.Header.Set(cloudTraceContextHeader, sampledCloudTraceContext)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	entry := decodeLogEntry(t, &sink)
	if entry["severity"] != "INFO" {
		t.Fatalf("severity = %v, want INFO", entry["severity"])
	}
	httpRequest, ok := entry["httpRequest"].(map[string]any)
	if !ok {
		t.Fatalf("httpRequest = %T, want a structured object", entry["httpRequest"])
	}
	if httpRequest["requestMethod"] != http.MethodGet {
		t.Fatalf("requestMethod = %v, want %s", httpRequest["requestMethod"], http.MethodGet)
	}
	if httpRequest["status"] != float64(http.StatusNoContent) {
		t.Fatalf("status = %v, want %d", httpRequest["status"], http.StatusNoContent)
	}

	wantTrace := "projects/attribution-chain-505000/traces/" + sampledCloudTraceID
	if entry["logging.googleapis.com/trace"] != wantTrace {
		t.Fatalf("trace = %v, want %s", entry["logging.googleapis.com/trace"], wantTrace)
	}
	if entry["logging.googleapis.com/trace_sampled"] != true {
		t.Fatalf("trace sampled = %v, want true", entry["logging.googleapis.com/trace_sampled"])
	}
}

func TestWrapHTTPRequestLogBoundsHTTPMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   string
	}{
		{name: "registered method", method: http.MethodPost, want: http.MethodPost},
		{name: "unregistered method", method: "PROPFIND", want: unknownHTTPMethod},
		{name: "lowercase method", method: "get", want: unknownHTTPMethod},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var sink bytes.Buffer
			runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(test.method, "/healthz", http.NoBody)
			handler.ServeHTTP(httptest.NewRecorder(), request)

			entry := decodeLogEntry(t, &sink)
			httpRequest, ok := entry[cloudLoggingHTTPRequestKey].(map[string]any)
			if !ok {
				t.Fatalf("httpRequest = %T, want a structured object", entry[cloudLoggingHTTPRequestKey])
			}
			if httpRequest["requestMethod"] != test.want {
				t.Fatalf("requestMethod = %v, want %s", httpRequest["requestMethod"], test.want)
			}
		})
	}
}

func TestWrapHTTPRequestLogExcludesSensitiveRequestValues(t *testing.T) {
	t.Parallel()

	const (
		secretQueryValue = "super-secret-token"
		secretCredential = "Bearer super-secret-credential"
		secretUserAgent  = "chain-test-agent/1.0"
		secretRemoteHost = "203.0.113.42"
	)

	var sink bytes.Buffer
	runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz?access_token="+secretQueryValue, http.NoBody)
	request.Header.Set("Authorization", secretCredential)
	request.Header.Set("User-Agent", secretUserAgent)
	request.RemoteAddr = secretRemoteHost + ":54321"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	logged := sink.String()
	if !strings.Contains(logged, requestCompletedMessage) {
		t.Fatalf("request log missing %q: %s", requestCompletedMessage, logged)
	}
	for _, forbidden := range []string{
		secretQueryValue,
		secretCredential,
		secretUserAgent,
		secretRemoteHost,
		"access_token",
		"/healthz",
	} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("request log leaked %q: %s", forbidden, logged)
		}
	}
}

func TestWrapHTTPRequestLogPreservesResponseControllerFlush(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var flushErr error
	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
		flushErr = http.NewResponseController(response).Flush()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if flushErr != nil {
		t.Fatalf("ResponseController.Flush() = %v, want nil", flushErr)
	}

	entry := decodeLogEntry(t, &sink)
	httpRequest, ok := entry[cloudLoggingHTTPRequestKey].(map[string]any)
	if !ok {
		t.Fatalf("httpRequest = %T, want a structured object", entry[cloudLoggingHTTPRequestKey])
	}
	if httpRequest["status"] != float64(http.StatusAccepted) {
		t.Fatalf("status = %v, want %d", httpRequest["status"], http.StatusAccepted)
	}
}

func TestWrapHTTPRecoversPanicAndLogsError(t *testing.T) {
	t.Parallel()

	const panicMessage = "simulated-panic-error-98471"
	var sink bytes.Buffer
	cfg := loadTestConfig(t, map[string]string{"CHAIN_GCP_PROJECT_ID": "attribution-chain-505000"})
	runtime, err := New(t.Context(), cfg, WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runtime.tracerProvider = newRecordingTracerProvider(t)

	handler := runtime.WrapHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(panicMessage)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	request.Header.Set(cloudTraceContextHeader, sampledCloudTraceContext)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() != "internal server error\n" {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), "internal server error\n")
	}

	decoder := json.NewDecoder(&sink)
	var panicLogEntry map[string]any
	if err := decoder.Decode(&panicLogEntry); err != nil {
		t.Fatalf("decode panic log entry: %v", err)
	}
	if panicLogEntry["severity"] != "ERROR" {
		t.Fatalf("panic entry severity = %v, want ERROR", panicLogEntry["severity"])
	}
	if panicLogEntry["message"] != "HTTP handler panic recovered" {
		t.Fatalf("panic entry message = %v, want 'HTTP handler panic recovered'", panicLogEntry["message"])
	}
	if panicLogEntry["error"] != panicMessage {
		t.Fatalf("panic entry error = %v, want %q", panicLogEntry["error"], panicMessage)
	}
	if stack, ok := panicLogEntry["stack"].(string); !ok || stack == "" {
		t.Fatalf("panic entry stack = %v, want non-empty string", panicLogEntry["stack"])
	}
	wantTrace := "projects/attribution-chain-505000/traces/" + sampledCloudTraceID
	if panicLogEntry["logging.googleapis.com/trace"] != wantTrace {
		t.Fatalf("panic entry trace = %v, want %s", panicLogEntry["logging.googleapis.com/trace"], wantTrace)
	}

	var requestLogEntry map[string]any
	if err := decoder.Decode(&requestLogEntry); err != nil {
		t.Fatalf("decode request log entry: %v", err)
	}
	if requestLogEntry["severity"] != "INFO" {
		t.Fatalf("request entry severity = %v, want INFO", requestLogEntry["severity"])
	}
	httpRequest, ok := requestLogEntry[cloudLoggingHTTPRequestKey].(map[string]any)
	if !ok {
		t.Fatalf("httpRequest = %T, want a structured object", requestLogEntry[cloudLoggingHTTPRequestKey])
	}
	if httpRequest["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("request log status = %v, want %d", httpRequest["status"], http.StatusInternalServerError)
	}
	if requestLogEntry["logging.googleapis.com/trace"] != wantTrace {
		t.Fatalf("request entry trace = %v, want %s", requestLogEntry["logging.googleapis.com/trace"], wantTrace)
	}
}

func TestWrapHTTPPanicRecoveryPassesThroughErrAbortHandler(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	runtime, err := New(t.Context(), loadTestConfig(t, nil), WithLogSink(zapcore.AddSync(&sink)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := runtime.WrapHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	recorder := httptest.NewRecorder()

	defer func() {
		rvr := recover()
		if rvr != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", rvr)
		}
		if sink.Len() != 0 {
			t.Fatalf("sink wrote %d bytes, want 0 on ErrAbortHandler: %s", sink.Len(), sink.String())
		}
	}()

	handler.ServeHTTP(recorder, request)
}

func newRecordingTracerProvider(t *testing.T) *sdktrace.TracerProvider {
	t.Helper()

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder()),
	)
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown test tracer provider: %v", err)
		}
	})
	return tracerProvider
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
