package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	cloudpropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"github.com/JakeFAU/chain-application/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	serviceNamespace          = "attribution-chain"
	httpServerSpanName        = "http.server.request"
	unknownHTTPMethod         = "OTHER"
	endpointSchemeHTTP        = "http"
	endpointSchemeHTTPS       = "https"
	cloudTraceContextHeader   = cloudpropagator.TraceContextHeaderName
	invalidEndpointReason     = "invalid OTLP endpoint"
	insecureEndpointReason    = "insecure OTLP endpoint requires a loopback host"
	originalRequestContextKey = runtimeContextKey(0)
)

type runtimeContextKey uint8

var cloudTraceContextPattern = regexp.MustCompile(`^[0-9a-f]{32}/[0-9]{1,20}(;o=[0-9])?$`)

// Option configures process-owned observability adapters.
type Option func(*options)

type options struct {
	logSink               zapcore.WriteSyncer
	traceExporterFactory  traceExporterFactory
	metricExporterFactory metricExporterFactory
}

type traceExporterFactory func(
	context.Context,
	...otlptracegrpc.Option,
) (sdktrace.SpanExporter, error)

type metricExporterFactory func(
	context.Context,
	...otlpmetricgrpc.Option,
) (sdkmetric.Exporter, error)

// WithLogSink replaces stdout as the application log sink.
func WithLogSink(sink zapcore.WriteSyncer) Option {
	return func(options *options) {
		options.logSink = sink
	}
}

// Runtime owns the application's logger and explicit telemetry providers.
type Runtime struct {
	logger         *zap.Logger
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	propagator     propagation.TextMapPropagator
	shutdown       func(context.Context) error
}

// New constructs the application logging and telemetry runtime.
func New(ctx context.Context, cfg config.Config, runtimeOptions ...Option) (*Runtime, error) {
	configuredOptions := defaultOptions()
	for _, option := range runtimeOptions {
		option(&configuredOptions)
	}

	logger, err := newLogger(cfg.LogLevel, configuredOptions.logSink)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	tracerProvider, meterProvider, propagator, shutdown, err := newTelemetry(
		ctx,
		cfg.Telemetry,
		configuredOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry: %w", err)
	}

	return &Runtime{
		logger:         logger,
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
		propagator:     propagator,
		shutdown:       shutdown,
	}, nil
}

// Logger returns the sole application logger.
func (runtime Runtime) Logger() *zap.Logger {
	return runtime.logger
}

// WrapHTTP instruments an HTTP handler without recording request-controlled values.
func (runtime Runtime) WrapHTTP(handler http.Handler) http.Handler {
	downstream := http.HandlerFunc(func(response http.ResponseWriter, telemetryRequest *http.Request) {
		originalRequest, ok := telemetryRequest.Context().Value(originalRequestContextKey).(*http.Request)
		if !ok {
			handler.ServeHTTP(response, telemetryRequest)
			return
		}

		handler.ServeHTTP(response, originalRequest.WithContext(telemetryRequest.Context()))
	})

	instrumented := otelhttp.NewHandler(
		downstream,
		httpServerSpanName,
		otelhttp.WithTracerProvider(runtime.tracerProvider),
		otelhttp.WithMeterProvider(runtime.meterProvider),
		otelhttp.WithPropagators(runtime.propagator),
		otelhttp.WithSpanNameFormatter(func(string, *http.Request) string {
			return httpServerSpanName
		}),
	)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), originalRequestContextKey, request)
		instrumented.ServeHTTP(response, telemetryRequest(request, ctx, runtime.propagator.Fields()))
	})
}

// Shutdown flushes and closes the telemetry resources owned by Runtime.
func (runtime Runtime) Shutdown(ctx context.Context) error {
	if runtime.shutdown == nil {
		return nil
	}
	return runtime.shutdown(ctx)
}

func newTelemetry(
	ctx context.Context,
	cfg config.Telemetry,
	configuredOptions options,
) (trace.TracerProvider, metric.MeterProvider, propagation.TextMapPropagator, func(context.Context) error, error) {
	propagator := newPropagator()
	if !cfg.Enabled {
		return tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider(), propagator, noOpShutdown, nil
	}

	endpoint, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	telemetryResource, err := newResource(cfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	traceOptions := []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(endpoint.String())}
	metricOptions := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(endpoint.String())}
	if endpoint.Scheme == endpointSchemeHTTP {
		traceOptions = append(traceOptions, otlptracegrpc.WithInsecure())
		metricOptions = append(metricOptions, otlpmetricgrpc.WithInsecure())
	}

	traceExporter, err := configuredOptions.traceExporterFactory(ctx, traceOptions...)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("create trace exporter: %w", err)
	}

	metricExporter, err := configuredOptions.metricExporterFactory(ctx, metricOptions...)
	if err != nil {
		return nil, nil, nil, nil, errors.Join(
			fmt.Errorf("create metric exporter: %w", err),
			traceExporter.Shutdown(ctx),
		)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(telemetryResource),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(telemetryResource),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)

	shutdown := func(ctx context.Context) error {
		return shutdownProviders(
			ctx,
			meterProvider.Shutdown,
			tracerProvider.Shutdown,
		)
	}

	return tracerProvider, meterProvider, propagator, shutdown, nil
}

func defaultOptions() options {
	return options{
		traceExporterFactory: func(
			ctx context.Context,
			exporterOptions ...otlptracegrpc.Option,
		) (sdktrace.SpanExporter, error) {
			return otlptracegrpc.New(ctx, exporterOptions...)
		},
		metricExporterFactory: func(
			ctx context.Context,
			exporterOptions ...otlpmetricgrpc.Option,
		) (sdkmetric.Exporter, error) {
			return otlpmetricgrpc.New(ctx, exporterOptions...)
		},
	}
}

func shutdownProviders(ctx context.Context, shutdowns ...func(context.Context) error) error {
	type shutdownResult struct {
		index int
		err   error
	}

	results := make(chan shutdownResult, len(shutdowns))
	for index, shutdown := range shutdowns {
		go func() {
			results <- shutdownResult{index: index, err: shutdown(ctx)}
		}()
	}

	errs := make([]error, len(shutdowns))
	for range shutdowns {
		result := <-results
		errs[result.index] = result.err
	}
	return errors.Join(errs...)
}

func newResource(cfg config.Telemetry) (*resource.Resource, error) {
	applicationResource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNamespace(serviceNamespace),
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
		semconv.DeploymentEnvironmentNameKey.String(string(cfg.Environment)),
	)
	telemetryResource, err := resource.Merge(resource.Default(), applicationResource)
	if err != nil {
		return nil, fmt.Errorf("merge telemetry resource: %w", err)
	}
	return telemetryResource, nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		cloudpropagator.CloudTraceOneWayPropagator{},
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func parseEndpoint(rawEndpoint string) (*url.URL, error) {
	endpoint, err := url.ParseRequestURI(rawEndpoint)
	if err != nil || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("%s", invalidEndpointReason)
	}
	if endpoint.Scheme != endpointSchemeHTTP && endpoint.Scheme != endpointSchemeHTTPS {
		return nil, fmt.Errorf("%s", invalidEndpointReason)
	}
	if endpoint.Scheme == endpointSchemeHTTP && !isLoopbackHost(endpoint.Hostname()) {
		return nil, fmt.Errorf("%s", insecureEndpointReason)
	}
	return endpoint, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func telemetryRequest(
	request *http.Request,
	ctx context.Context,
	propagationFields []string,
) *http.Request {
	telemetryRequest := request.Clone(ctx)
	telemetryRequest.Method = boundedHTTPMethod(request.Method)
	telemetryRequest.URL = &url.URL{}
	telemetryRequest.Host = ""
	telemetryRequest.RemoteAddr = ""
	telemetryRequest.Header = make(http.Header, len(propagationFields))
	for _, field := range propagationFields {
		for _, value := range request.Header.Values(field) {
			telemetryRequest.Header.Add(field, value)
		}
	}
	cloudTraceContext := request.Header.Get(cloudTraceContextHeader)
	if validCloudTraceContext(cloudTraceContext) {
		telemetryRequest.Header.Set(cloudTraceContextHeader, cloudTraceContext)
	}
	telemetryRequest.Body = http.NoBody
	telemetryRequest.ContentLength = 0
	return telemetryRequest
}

func validCloudTraceContext(header string) bool {
	if !cloudTraceContextPattern.MatchString(header) {
		return false
	}

	separator := strings.IndexByte(header, '/')
	traceID, err := trace.TraceIDFromHex(header[:separator])
	if err != nil || !traceID.IsValid() {
		return false
	}

	spanID := header[separator+1:]
	if options := strings.IndexByte(spanID, ';'); options >= 0 {
		spanID = spanID[:options]
	}
	parsedSpanID, err := strconv.ParseUint(spanID, 10, 64)
	return err == nil && parsedSpanID != 0
}

func boundedHTTPMethod(method string) string {
	switch method {
	case http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return unknownHTTPMethod
	}
}

func noOpShutdown(context.Context) error {
	return nil
}
