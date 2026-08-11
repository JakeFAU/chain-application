package observability

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	cloudLoggingTraceKey        = "logging.googleapis.com/trace"
	cloudLoggingSpanIDKey       = "logging.googleapis.com/spanId"
	cloudLoggingTraceSampledKey = "logging.googleapis.com/trace_sampled"
)

var projectIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// TraceFields returns Cloud Logging correlation fields for a valid current span.
func TraceFields(ctx context.Context, projectID string) []zap.Field {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() || !projectIDPattern.MatchString(projectID) {
		return nil
	}

	return []zap.Field{
		zap.String(
			cloudLoggingTraceKey,
			"projects/"+projectID+"/traces/"+spanContext.TraceID().String(),
		),
		zap.String(cloudLoggingSpanIDKey, spanContext.SpanID().String()),
		zap.Bool(cloudLoggingTraceSampledKey, spanContext.IsSampled()),
	}
}
