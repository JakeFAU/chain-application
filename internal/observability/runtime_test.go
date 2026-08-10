package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

func TestDisabledRuntimeWrapsAndShutsDownWithoutGlobalMutation(t *testing.T) {
	t.Parallel()

	globalTracerProvider := otel.GetTracerProvider()
	globalMeterProvider := otel.GetMeterProvider()
	globalPropagator := otel.GetTextMapPropagator()

	runtime, err := New(
		t.Context(),
		loadTestConfig(t, nil),
		WithLogSink(zapcore.AddSync(&bytes.Buffer{})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if otel.GetTracerProvider() != globalTracerProvider {
		t.Fatal("global tracer provider changed")
	}
	if otel.GetMeterProvider() != globalMeterProvider {
		t.Fatal("global meter provider changed")
	}
	if pointerOf(otel.GetTextMapPropagator()) != pointerOf(globalPropagator) {
		t.Fatal("global text map propagator changed")
	}
}

func TestDisabledRuntimeWrapHTTPUsesBoundedTelemetry(t *testing.T) {
	t.Parallel()

	runtime, err := New(
		t.Context(),
		loadTestConfig(t, nil),
		WithLogSink(zapcore.AddSync(&bytes.Buffer{})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown test tracer provider: %v", err)
		}
	})
	runtime.tracerProvider = tracerProvider
	runtime.meterProvider = noop.NewMeterProvider()

	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/subjects/private-id-123" {
			t.Errorf("downstream path = %q, want original path", request.URL.Path)
		}
		if request.URL.RawQuery != "token=private-query-456" {
			t.Errorf("downstream query = %q, want original query", request.URL.RawQuery)
		}
		if request.UserAgent() != "private-agent-789" {
			t.Errorf("downstream user agent = %q, want original user agent", request.UserAgent())
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read downstream body: %v", err)
		}
		if string(body) != "private-body-012" {
			t.Errorf("downstream body = %q, want original body", body)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(
		http.MethodPost,
		"/subjects/private-id-123?token=private-query-456",
		strings.NewReader("private-body-012"),
	)
	request.Header.Set("User-Agent", "private-agent-789")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Name() != "http.server.request" {
		t.Fatalf("span name = %q, want http.server.request", spans[0].Name())
	}

	for _, field := range spans[0].Attributes() {
		value := field.Value.String()
		for _, forbidden := range []string{
			"private-id-123",
			"private-query-456",
			"private-agent-789",
			"private-body-012",
		} {
			if strings.Contains(value, forbidden) {
				t.Fatalf("attribute %q contains private value %q", field.Key, forbidden)
			}
		}
	}
}

func TestDisabledRuntimeW3CPropagationWins(t *testing.T) {
	t.Parallel()

	runtime, err := New(
		t.Context(),
		loadTestConfig(t, nil),
		WithLogSink(zapcore.AddSync(&bytes.Buffer{})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	carrier := propagation.MapCarrier{
		"X-Cloud-Trace-Context": "0123456789abcdef0123456789abcdef/81985529216486895;o=1",
		"traceparent":           "00-fedcba9876543210fedcba9876543210-fedcba9876543210-01",
	}
	ctx := runtime.propagator.Extract(context.Background(), carrier)
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.TraceID().String() != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("trace ID = %s, want W3C trace ID", spanContext.TraceID())
	}
	if spanContext.SpanID().String() != "fedcba9876543210" {
		t.Fatalf("span ID = %s, want W3C span ID", spanContext.SpanID())
	}
}

func TestDisabledRuntimeWrapHTTPExtractsCloudTraceContext(t *testing.T) {
	t.Parallel()

	runtime, err := New(
		t.Context(),
		loadTestConfig(t, nil),
		WithLogSink(zapcore.AddSync(&bytes.Buffer{})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown test tracer provider: %v", err)
		}
	})
	runtime.tracerProvider = tracerProvider

	var downstreamSpanContext trace.SpanContext
	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		downstreamSpanContext = trace.SpanContextFromContext(request.Context())
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	request.Header.Set(
		"X-Cloud-Trace-Context",
		"0123456789abcdef0123456789abcdef/81985529216486895;o=1",
	)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if downstreamSpanContext.TraceID().String() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("trace ID = %s, want extracted Cloud Trace ID", downstreamSpanContext.TraceID())
	}
}

func TestOTLPEndpointTransportSecurity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		endpoint  string
		wantError bool
	}{
		{name: "localhost HTTP", endpoint: "http://localhost:4317"},
		{name: "IPv4 loopback HTTP", endpoint: "http://127.0.0.1:4317"},
		{name: "IPv6 loopback HTTP", endpoint: "http://[::1]:4317"},
		{name: "remote HTTPS", endpoint: "https://collector.example.com:4317"},
		{name: "remote HTTP", endpoint: "http://collector.example.com:4317", wantError: true},
		{name: "lookalike host HTTP", endpoint: "http://localhost.example.com:4317", wantError: true},
		{name: "missing scheme", endpoint: "localhost:4317", wantError: true},
		{name: "malformed URL", endpoint: "://localhost:4317", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseEndpoint(test.endpoint)
			if (err != nil) != test.wantError {
				t.Fatalf("parseEndpoint(%q) error = %v, wantError %t", test.endpoint, err, test.wantError)
			}
		})
	}
}

func TestRuntimeShutdownJoinsOwnedErrors(t *testing.T) {
	t.Parallel()

	first := errors.New("first shutdown")
	second := errors.New("second shutdown")
	runtime := Runtime{
		shutdown: func(context.Context) error {
			return errors.Join(first, second)
		},
	}

	err := runtime.Shutdown(t.Context())
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("Shutdown error = %v, want both owned errors", err)
	}
}

func pointerOf(value any) uintptr {
	return reflect.ValueOf(value).Pointer()
}
