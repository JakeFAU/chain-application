package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
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

func TestWrapHTTPDropsMalformedCloudTraceWithoutProcessOutput(t *testing.T) {
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

	var downstreamSpanContext trace.SpanContext
	handler := runtime.WrapHTTP(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		downstreamSpanContext = trace.SpanContextFromContext(request.Context())
		response.WriteHeader(http.StatusNoContent)
	}))
	stdout, stderr, standardLog := captureProcessOutput(t, func() {
		malformedHeaders := []string{
			"private-cloud-header-secret-012",
			"0123456789abcdef0123456789abcdef/18446744073709551616;o=1",
			"00000000000000000000000000000000/81985529216486895;o=1",
			"0123456789abcdef0123456789abcdef/0;o=1",
		}
		for _, malformedHeader := range malformedHeaders {
			request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			request.Header.Set(cloudTraceContextHeader, malformedHeader)
			request.Header.Set(
				"traceparent",
				"00-fedcba9876543210fedcba9876543210-fedcba9876543210-01",
			)
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}
	})

	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if standardLog != "" {
		t.Fatalf("standard log = %q, want empty", standardLog)
	}
	if downstreamSpanContext.TraceID().String() != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("trace ID = %s, want W3C trace ID", downstreamSpanContext.TraceID())
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

func TestEnabledRuntimeOwnsTraceAndMetricPipelines(t *testing.T) {
	t.Parallel()

	traceExporter := newRecordingTraceExporter()
	metricExporter := newRecordingMetricExporter()
	var traceFactoryCalls atomic.Int32
	var metricFactoryCalls atomic.Int32
	cfg := loadTestConfig(t, map[string]string{
		"CHAIN_OTEL_ENABLED":          "true",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
	})

	runtime, err := New(
		t.Context(),
		cfg,
		WithLogSink(zapcore.AddSync(&bytes.Buffer{})),
		Option(func(options *options) {
			options.traceExporterFactory = func(
				context.Context,
				...otlptracegrpc.Option,
			) (sdktrace.SpanExporter, error) {
				traceFactoryCalls.Add(1)
				return traceExporter, nil
			}
			options.metricExporterFactory = func(
				context.Context,
				...otlpmetricgrpc.Option,
			) (sdkmetric.Exporter, error) {
				metricFactoryCalls.Add(1)
				return metricExporter, nil
			}
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	shutdownCalled := false
	t.Cleanup(func() {
		if shutdownCalled {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf("cleanup Shutdown: %v", err)
		}
	})

	_, span := runtime.tracerProvider.Tracer("enabled-runtime-test").Start(t.Context(), "owned-span")
	span.End()
	counter, err := runtime.meterProvider.Meter("enabled-runtime-test").Int64Counter("owned.counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(t.Context(), 1)

	shutdownCalled = true
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if traceFactoryCalls.Load() != 1 {
		t.Fatalf("trace exporter factory calls = %d, want 1", traceFactoryCalls.Load())
	}
	if metricFactoryCalls.Load() != 1 {
		t.Fatalf("metric exporter factory calls = %d, want 1", metricFactoryCalls.Load())
	}
	if traceExporter.exportedSpans.Load() != 1 {
		t.Fatalf("exported spans = %d, want 1", traceExporter.exportedSpans.Load())
	}
	if metricExporter.exports.Load() == 0 {
		t.Fatal("metric exports = 0, want at least 1")
	}
	if traceExporter.shutdowns.Load() != 1 {
		t.Fatalf("trace exporter shutdowns = %d, want 1", traceExporter.shutdowns.Load())
	}
	if metricExporter.shutdowns.Load() != 1 {
		t.Fatalf("metric exporter shutdowns = %d, want 1", metricExporter.shutdowns.Load())
	}

	traceResource := <-traceExporter.resources
	assertResourceAttribute(t, traceResource, "service.namespace", "attribution-chain")
	assertResourceAttribute(t, traceResource, "service.name", "attribution-chain-api")
	assertResourceAttribute(t, traceResource, "service.version", "test-version")
	assertResourceAttribute(t, traceResource, "deployment.environment.name", "local")
}

func TestShutdownProvidersStartsBothWithLiveContextAndJoinsErrors(t *testing.T) {
	first := errors.New("first shutdown")
	second := errors.New("second shutdown")
	secondStarted := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	firstShutdown := func(ctx context.Context) error {
		select {
		case <-secondStarted:
			return first
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	secondShutdown := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		close(secondStarted)
		return second
	}

	err := shutdownProviders(ctx, firstShutdown, secondShutdown)
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("shutdownProviders error = %v, want both owned errors", err)
	}
}

func pointerOf(value any) uintptr {
	return reflect.ValueOf(value).Pointer()
}

func captureProcessOutput(t *testing.T, run func()) (string, string, string) {
	t.Helper()

	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	originalLogWriter := log.Writer()
	var standardLog bytes.Buffer
	restored := false
	restore := func() {
		if restored {
			return
		}
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		log.SetOutput(originalLogWriter)
		restored = true
	}
	defer restore()

	os.Stdout = stdoutFile
	os.Stderr = stderrFile
	log.SetOutput(&standardLog)
	run()
	restore()

	stdout := readCapture(t, stdoutFile)
	stderr := readCapture(t, stderrFile)
	return stdout, stderr, standardLog.String()
}

func readCapture(t *testing.T, file *os.File) string {
	t.Helper()

	if err := file.Close(); err != nil {
		t.Fatalf("close capture: %v", err)
	}
	content, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return string(content)
}

type recordingTraceExporter struct {
	exportedSpans atomic.Int32
	shutdowns     atomic.Int32
	resources     chan *resource.Resource
}

func newRecordingTraceExporter() *recordingTraceExporter {
	return &recordingTraceExporter{resources: make(chan *resource.Resource, 1)}
}

func (exporter *recordingTraceExporter) ExportSpans(
	_ context.Context,
	spans []sdktrace.ReadOnlySpan,
) error {
	exporter.exportedSpans.Add(int32(len(spans)))
	if len(spans) > 0 {
		select {
		case exporter.resources <- spans[0].Resource():
		default:
		}
	}
	return nil
}

func (exporter *recordingTraceExporter) Shutdown(context.Context) error {
	exporter.shutdowns.Add(1)
	return nil
}

type recordingMetricExporter struct {
	exports   atomic.Int32
	shutdowns atomic.Int32
}

func newRecordingMetricExporter() *recordingMetricExporter {
	return &recordingMetricExporter{}
}

func (*recordingMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (*recordingMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (exporter *recordingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	exporter.exports.Add(1)
	return nil
}

func (*recordingMetricExporter) ForceFlush(context.Context) error {
	return nil
}

func (exporter *recordingMetricExporter) Shutdown(context.Context) error {
	exporter.shutdowns.Add(1)
	return nil
}

func assertResourceAttribute(t *testing.T, telemetryResource *resource.Resource, key, want string) {
	t.Helper()

	value, ok := telemetryResource.Set().Value(attribute.Key(key))
	if !ok {
		t.Fatalf("resource attribute %q is missing", key)
	}
	if value.AsString() != want {
		t.Fatalf("resource attribute %q = %q, want %q", key, value.AsString(), want)
	}
}
