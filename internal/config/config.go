package config

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	environmentPort                  = "PORT"
	environmentLogLevel              = "CHAIN_LOG_LEVEL"
	environmentShutdownTimeout       = "CHAIN_SHUTDOWN_TIMEOUT"
	environmentTelemetryEnabled      = "CHAIN_OTEL_ENABLED"
	environmentTraceSampleRatio      = "CHAIN_OTEL_TRACE_SAMPLE_RATIO"
	environmentOTELTracesSampler     = "OTEL_TRACES_SAMPLER"
	environmentOTELTracesSamplerArg  = "OTEL_TRACES_SAMPLER_ARG"
	environmentTelemetryEndpoint     = "OTEL_EXPORTER_OTLP_ENDPOINT"
	environmentProjectID             = "CHAIN_GCP_PROJECT_ID"
	environmentDeploymentEnvironment = "CHAIN_DEPLOYMENT_ENVIRONMENT"

	defaultPort                  = "8080"
	defaultLogLevel              = "info"
	defaultShutdownTimeout       = "8s"
	defaultTelemetryEnabled      = "false"
	defaultTraceSampleRatio      = "1.0"
	defaultDeploymentEnvironment = "local"
	defaultServiceName           = "attribution-chain-api"

	minimumPort = 1
	maximumPort = 65535

	minimumShutdownTimeout = time.Second
	maximumShutdownTimeout = 9 * time.Second

	invalidPortReason     = "invalid port"
	invalidBooleanReason  = "invalid boolean"
	invalidDurationReason = "invalid duration"
	invalidRatioReason    = "invalid trace sample ratio"
)

// LookupEnv reads one environment variable at the startup boundary.
type LookupEnv func(string) (string, bool)

// LogLevel is the bounded set of supported application log levels.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// DeploymentEnvironment identifies the bounded deployment context for telemetry.
type DeploymentEnvironment string

const (
	EnvironmentLocal       DeploymentEnvironment = "local"
	EnvironmentDevelopment DeploymentEnvironment = "development"
	EnvironmentProduction  DeploymentEnvironment = "production"
)

// TraceSampleRatio controls sampling for new root traces.
type TraceSampleRatio float64

const (
	minimumTraceSampleRatio TraceSampleRatio = 0
	maximumTraceSampleRatio TraceSampleRatio = 1
)

// Telemetry contains observability configuration validated at startup.
type Telemetry struct {
	Enabled          bool
	TraceSampleRatio TraceSampleRatio
	Endpoint         string
	ProjectID        string
	Environment      DeploymentEnvironment
	ServiceName      string
	Version          string
}

// Config contains all typed application startup configuration.
type Config struct {
	Port            uint16
	LogLevel        LogLevel
	ShutdownTimeout time.Duration
	Telemetry       Telemetry
}

// Address returns the listener address for the configured port on every interface.
func (config Config) Address() string {
	return net.JoinHostPort("", strconv.FormatUint(uint64(config.Port), 10))
}

// Load parses and validates startup configuration through the injected lookup.
func Load(lookup LookupEnv, buildVersion string) (Config, error) {
	port, err := loadPort(lookup)
	if err != nil {
		return Config{}, err
	}

	logLevel, err := loadLogLevel(lookup)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := loadShutdownTimeout(lookup)
	if err != nil {
		return Config{}, err
	}

	telemetry, err := loadTelemetry(lookup, buildVersion)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:            port,
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
		Telemetry:       telemetry,
	}, nil
}

func loadPort(lookup LookupEnv) (uint16, error) {
	value := lookupOrDefault(lookup, environmentPort, defaultPort)
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%s: %s", environmentPort, invalidPortReason)
	}
	if port < minimumPort || port > maximumPort {
		return 0, fmt.Errorf("%s must be between %d and %d", environmentPort, minimumPort, maximumPort)
	}

	return uint16(port), nil
}

func loadLogLevel(lookup LookupEnv) (LogLevel, error) {
	logLevel := LogLevel(lookupOrDefault(lookup, environmentLogLevel, defaultLogLevel))
	switch logLevel {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return logLevel, nil
	default:
		return "", fmt.Errorf("%s: unsupported log level", environmentLogLevel)
	}
}

func loadShutdownTimeout(lookup LookupEnv) (time.Duration, error) {
	value := lookupOrDefault(lookup, environmentShutdownTimeout, defaultShutdownTimeout)
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %s", environmentShutdownTimeout, invalidDurationReason)
	}
	if timeout <= minimumShutdownTimeout || timeout > maximumShutdownTimeout {
		return 0, fmt.Errorf(
			"%s must be greater than %s and at most %s",
			environmentShutdownTimeout,
			minimumShutdownTimeout,
			maximumShutdownTimeout,
		)
	}

	return timeout, nil
}

func loadTelemetry(lookup LookupEnv, buildVersion string) (Telemetry, error) {
	enabled, err := strconv.ParseBool(lookupOrDefault(lookup, environmentTelemetryEnabled, defaultTelemetryEnabled))
	if err != nil {
		return Telemetry{}, fmt.Errorf("%s: %s", environmentTelemetryEnabled, invalidBooleanReason)
	}
	if enabled {
		if err := rejectAmbientOTELSamplerConfiguration(lookup); err != nil {
			return Telemetry{}, err
		}
	}
	traceSampleRatio, err := loadTraceSampleRatio(lookup)
	if err != nil {
		return Telemetry{}, err
	}

	environment, err := loadDeploymentEnvironment(lookup)
	if err != nil {
		return Telemetry{}, err
	}

	endpoint := lookupOrDefault(lookup, environmentTelemetryEndpoint, "")
	if enabled && endpoint == "" {
		return Telemetry{}, fmt.Errorf("%s is required when %s is enabled", environmentTelemetryEndpoint, environmentTelemetryEnabled)
	}

	projectID := lookupOrDefault(lookup, environmentProjectID, "")
	if environment == EnvironmentProduction && projectID == "" {
		return Telemetry{}, fmt.Errorf("%s is required in production", environmentProjectID)
	}

	return Telemetry{
		Enabled:          enabled,
		TraceSampleRatio: traceSampleRatio,
		Endpoint:         endpoint,
		ProjectID:        projectID,
		Environment:      environment,
		ServiceName:      defaultServiceName,
		Version:          buildVersion,
	}, nil
}

func rejectAmbientOTELSamplerConfiguration(lookup LookupEnv) error {
	for _, variable := range [...]string{
		environmentOTELTracesSampler,
		environmentOTELTracesSamplerArg,
	} {
		if _, present := lookup(variable); present {
			return fmt.Errorf(
				"%s is unsupported; use %s",
				variable,
				environmentTraceSampleRatio,
			)
		}
	}
	return nil
}

func loadTraceSampleRatio(lookup LookupEnv) (TraceSampleRatio, error) {
	value := lookupOrDefault(lookup, environmentTraceSampleRatio, defaultTraceSampleRatio)
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0, fmt.Errorf("%s: %s", environmentTraceSampleRatio, invalidRatioReason)
	}

	typedRatio := TraceSampleRatio(ratio)
	if typedRatio < minimumTraceSampleRatio || typedRatio > maximumTraceSampleRatio {
		return 0, fmt.Errorf(
			"%s must be between %g and %g",
			environmentTraceSampleRatio,
			minimumTraceSampleRatio,
			maximumTraceSampleRatio,
		)
	}
	return typedRatio, nil
}

func loadDeploymentEnvironment(lookup LookupEnv) (DeploymentEnvironment, error) {
	environment := DeploymentEnvironment(lookupOrDefault(lookup, environmentDeploymentEnvironment, defaultDeploymentEnvironment))
	switch environment {
	case EnvironmentLocal, EnvironmentDevelopment, EnvironmentProduction:
		return environment, nil
	default:
		return "", fmt.Errorf("%s: unsupported deployment environment", environmentDeploymentEnvironment)
	}
}

func lookupOrDefault(lookup LookupEnv, key, defaultValue string) string {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return defaultValue
	}

	return strings.TrimSpace(value)
}
