package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values map[string]string
	}{
		{
			name: "absent values",
		},
		{
			name: "empty values",
			values: map[string]string{
				"PORT":                         " ",
				"CHAIN_LOG_LEVEL":              " ",
				"CHAIN_SHUTDOWN_TIMEOUT":       " ",
				"CHAIN_OTEL_ENABLED":           " ",
				"CHAIN_DEPLOYMENT_ENVIRONMENT": " ",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config, err := Load(mapLookup(test.values), "test-version")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if config.Port != 8080 {
				t.Fatalf("Port = %d, want 8080", config.Port)
			}
			if config.Address() != ":8080" {
				t.Fatalf("Address = %q, want :8080", config.Address())
			}
			if config.LogLevel != LogLevel("info") {
				t.Fatalf("LogLevel = %q, want info", config.LogLevel)
			}
			if config.ShutdownTimeout != 8*time.Second {
				t.Fatalf("ShutdownTimeout = %s, want 8s", config.ShutdownTimeout)
			}
			if config.Telemetry.Enabled {
				t.Fatal("Telemetry.Enabled = true, want false")
			}
			if config.Telemetry.Environment != DeploymentEnvironment("local") {
				t.Fatalf("Telemetry.Environment = %q, want local", config.Telemetry.Environment)
			}
			if config.Telemetry.ServiceName != "attribution-chain-api" {
				t.Fatalf("ServiceName = %q, want attribution-chain-api", config.Telemetry.ServiceName)
			}
			if config.Telemetry.Version != "test-version" {
				t.Fatalf("Version = %q, want test-version", config.Telemetry.Version)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  map[string]string
		wantEnv string
	}{
		{
			name:    "zero port",
			values:  map[string]string{"PORT": "0"},
			wantEnv: "PORT",
		},
		{
			name:    "port above uint16 range",
			values:  map[string]string{"PORT": "65536"},
			wantEnv: "PORT",
		},
		{
			name:    "non-numeric port",
			values:  map[string]string{"PORT": "not-a-port"},
			wantEnv: "PORT",
		},
		{
			name:    "unsupported log level",
			values:  map[string]string{"CHAIN_LOG_LEVEL": "trace"},
			wantEnv: "CHAIN_LOG_LEVEL",
		},
		{
			name:    "invalid telemetry boolean",
			values:  map[string]string{"CHAIN_OTEL_ENABLED": "sometimes"},
			wantEnv: "CHAIN_OTEL_ENABLED",
		},
		{
			name:    "shutdown timeout at lower bound",
			values:  map[string]string{"CHAIN_SHUTDOWN_TIMEOUT": "1s"},
			wantEnv: "CHAIN_SHUTDOWN_TIMEOUT",
		},
		{
			name:    "shutdown timeout above upper bound",
			values:  map[string]string{"CHAIN_SHUTDOWN_TIMEOUT": "10s"},
			wantEnv: "CHAIN_SHUTDOWN_TIMEOUT",
		},
		{
			name:    "unsupported deployment environment",
			values:  map[string]string{"CHAIN_DEPLOYMENT_ENVIRONMENT": "staging"},
			wantEnv: "CHAIN_DEPLOYMENT_ENVIRONMENT",
		},
		{
			name: "enabled telemetry without endpoint",
			values: map[string]string{
				"CHAIN_OTEL_ENABLED": "true",
			},
			wantEnv: "OTEL_EXPORTER_OTLP_ENDPOINT",
		},
		{
			name: "production without project ID",
			values: map[string]string{
				"CHAIN_DEPLOYMENT_ENVIRONMENT": "production",
			},
			wantEnv: "CHAIN_GCP_PROJECT_ID",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(mapLookup(test.values), "test-version")
			if err == nil {
				t.Fatal("Load error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantEnv) {
				t.Fatalf("Load error = %q, want environment variable %q", err, test.wantEnv)
			}
		})
	}
}

func TestLoadParseErrorsDoNotExposeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		values     map[string]string
		wantEnv    string
		wantReason string
		sentinel   string
	}{
		{
			name:       "port",
			values:     map[string]string{"PORT": "secret-port-value-5f3f95b3"},
			wantEnv:    "PORT",
			wantReason: "invalid port",
			sentinel:   "secret-port-value-5f3f95b3",
		},
		{
			name:       "telemetry boolean",
			values:     map[string]string{"CHAIN_OTEL_ENABLED": "secret-boolean-value-093f70dd"},
			wantEnv:    "CHAIN_OTEL_ENABLED",
			wantReason: "invalid boolean",
			sentinel:   "secret-boolean-value-093f70dd",
		},
		{
			name:       "shutdown timeout",
			values:     map[string]string{"CHAIN_SHUTDOWN_TIMEOUT": "secret-duration-value-1ae269d6"},
			wantEnv:    "CHAIN_SHUTDOWN_TIMEOUT",
			wantReason: "invalid duration",
			sentinel:   "secret-duration-value-1ae269d6",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(mapLookup(test.values), "test-version")
			if err == nil {
				t.Fatal("Load error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantEnv) {
				t.Fatalf("Load error = %q, want environment variable %q", err, test.wantEnv)
			}
			if strings.Contains(err.Error(), test.sentinel) {
				t.Fatalf("Load error = %q, must not expose supplied value", err)
			}
			if !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("Load error = %q, want reason %q", err, test.wantReason)
			}
		})
	}
}

func TestLoadEnablesTelemetryWithTrimmedConfiguration(t *testing.T) {
	t.Parallel()

	config, err := Load(mapLookup(map[string]string{
		"PORT":                         " 18080 ",
		"CHAIN_LOG_LEVEL":              " warn ",
		"CHAIN_SHUTDOWN_TIMEOUT":       " 9s ",
		"CHAIN_OTEL_ENABLED":           " true ",
		"OTEL_EXPORTER_OTLP_ENDPOINT":  " http://localhost:4317 ",
		"CHAIN_GCP_PROJECT_ID":         " attribution-chain-505000 ",
		"CHAIN_DEPLOYMENT_ENVIRONMENT": " production ",
	}), "test-version")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.Port != 18080 {
		t.Fatalf("Port = %d, want 18080", config.Port)
	}
	if config.Address() != ":18080" {
		t.Fatalf("Address = %q, want :18080", config.Address())
	}
	if config.LogLevel != LogLevel("warn") {
		t.Fatalf("LogLevel = %q, want warn", config.LogLevel)
	}
	if config.ShutdownTimeout != 9*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 9s", config.ShutdownTimeout)
	}
	if !config.Telemetry.Enabled {
		t.Fatal("Telemetry.Enabled = false, want true")
	}
	if config.Telemetry.Endpoint != "http://localhost:4317" {
		t.Fatalf("Telemetry.Endpoint = %q, want http://localhost:4317", config.Telemetry.Endpoint)
	}
	if config.Telemetry.ProjectID != "attribution-chain-505000" {
		t.Fatalf("Telemetry.ProjectID = %q, want attribution-chain-505000", config.Telemetry.ProjectID)
	}
	if config.Telemetry.Environment != DeploymentEnvironment("production") {
		t.Fatalf("Telemetry.Environment = %q, want production", config.Telemetry.Environment)
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
