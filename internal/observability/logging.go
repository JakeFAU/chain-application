package observability

import (
	"fmt"
	"log"
	"os"

	"github.com/JakeFAU/chain-application/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	unsupportedLogLevelReason   = "unsupported log level"
	httpServerDiagnosticMessage = "HTTP server diagnostic"
)

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
