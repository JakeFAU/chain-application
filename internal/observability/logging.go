package observability

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/JakeFAU/chain-application/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	unsupportedLogLevelReason   = "unsupported log level"
	httpServerDiagnosticMessage = "HTTP server diagnostic"
	requestCompletedMessage     = "HTTP request completed"
	panicRecoveredMessage       = "HTTP handler panic recovered"
	internalServerErrorMessage  = "internal server error"
	cloudLoggingHTTPRequestKey  = "httpRequest"
	latencySecondsPrecision     = 9
)

// statusRecorder captures the response status for bounded request logging
// without retaining any response bytes.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the wrapped writer so http.ResponseController keeps reaching
// optional interfaces such as Flusher through this recorder.
func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}

// serveLogged serves one request and emits a single bounded log entry that
// carries Cloud Logging trace correlation when a valid span context exists.
// Only low-cardinality, non-sensitive fields are recorded: no URL, body,
// headers, remote address, or user agent.
func (runtime Runtime) serveLogged(
	response http.ResponseWriter,
	request *http.Request,
	handler http.Handler,
) {
	recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
	started := time.Now()
	defer func() {
		if rvr := recover(); rvr != nil {
			if rvr == http.ErrAbortHandler {
				panic(rvr)
			}
			runtime.logger.Error(
				panicRecoveredMessage,
				append(
					[]zap.Field{
						zap.Any("error", rvr),
						zap.Stack("stack"),
					},
					TraceFields(request.Context(), runtime.projectID)...,
				)...,
			)
			http.Error(recorder, internalServerErrorMessage, http.StatusInternalServerError)
		}

		elapsed := time.Since(started)
		fields := []zap.Field{
			zap.Dict(
				cloudLoggingHTTPRequestKey,
				zap.String("requestMethod", boundedHTTPMethod(request.Method)),
				zap.Int("status", recorder.status),
				zap.String("latency", formatLatencySeconds(elapsed)),
			),
		}
		runtime.logger.Info(
			requestCompletedMessage,
			append(fields, TraceFields(request.Context(), runtime.projectID)...)...,
		)
	}()

	handler.ServeHTTP(recorder, request)
}

func formatLatencySeconds(elapsed time.Duration) string {
	return strconv.FormatFloat(elapsed.Seconds(), 'f', latencySecondsPrecision, 64) + "s"
}

type httpServerLogWriter struct {
	logger *zap.Logger
}

// HTTPServerErrorLog bridges standard-library HTTP diagnostics into the
// process logger without retaining or emitting diagnostic bytes.
func (runtime Runtime) HTTPServerErrorLog() *log.Logger {
	return log.New(httpServerLogWriter{logger: runtime.logger}, "", 0)
}

func (writer httpServerLogWriter) Write(diagnostic []byte) (int, error) {
	writer.logger.Warn(httpServerDiagnosticMessage)
	return len(diagnostic), nil
}

func newLogger(level config.LogLevel, sink zapcore.WriteSyncer) (*zap.Logger, error) {
	zapLevel, err := parseLogLevel(level)
	if err != nil {
		return nil, err
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.MessageKey = "message"
	encoderConfig.LevelKey = "severity"
	encoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	encoderConfig.EncodeLevel = cloudLoggingLevelEncoder

	if sink == nil {
		sink = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.Lock(sink),
		zapLevel,
	)
	return zap.New(
		core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.ErrorOutput(zapcore.Lock(zapcore.AddSync(os.Stderr))),
	), nil
}

func parseLogLevel(level config.LogLevel) (zapcore.Level, error) {
	switch level {
	case config.LogLevelDebug:
		return zapcore.DebugLevel, nil
	case config.LogLevelInfo:
		return zapcore.InfoLevel, nil
	case config.LogLevelWarn:
		return zapcore.WarnLevel, nil
	case config.LogLevelError:
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InvalidLevel, fmt.Errorf("%s", unsupportedLogLevelReason)
	}
}

func cloudLoggingLevelEncoder(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	switch level {
	case zapcore.DebugLevel:
		encoder.AppendString("DEBUG")
	case zapcore.InfoLevel:
		encoder.AppendString("INFO")
	case zapcore.WarnLevel:
		encoder.AppendString("WARNING")
	case zapcore.ErrorLevel:
		encoder.AppendString("ERROR")
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		encoder.AppendString("CRITICAL")
	default:
		encoder.AppendString("ERROR")
	}
}
