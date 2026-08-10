package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/JakeFAU/chain-application/internal/app"
	"github.com/JakeFAU/chain-application/internal/config"
	"github.com/JakeFAU/chain-application/internal/httpapi"
	"github.com/JakeFAU/chain-application/internal/observability"
	"go.uber.org/zap"
)

const (
	exitSuccess               = 0
	exitFailure               = 1
	maximumFallbackErrorBytes = 1024
	fallbackPrefix            = "chain-api: "

	listenNetwork                  = "tcp"
	listenFailureMessage           = "HTTP listener startup failed"
	runtimeFailureMessage          = "application runtime failed"
	telemetryCleanupFailureMessage = "telemetry cleanup failed"
	standardOutputPath             = "/dev/stdout"
	standardErrorPath              = "/dev/stderr"
)

var buildVersion = "devel"

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.LookupEnv, buildVersion)
	if err != nil {
		writeFallback(os.Stderr, fmt.Errorf("load configuration: %w", err))
		return exitFailure
	}

	observabilityRuntime, err := observability.New(context.Background(), cfg)
	if err != nil {
		writeFallback(os.Stderr, fmt.Errorf("construct observability: %w", err))
		return exitFailure
	}
	logger := observabilityRuntime.Logger()

	handler := httpapi.NewHandler(&httpapi.Server{}, observabilityRuntime.WrapHTTP)
	listener, err := net.Listen(listenNetwork, cfg.Address())
	if err != nil {
		logger.Error(listenFailureMessage, zap.Error(err))
		cleanupRuntimeAfterStartupFailure(cfg, observabilityRuntime, logger)
		return exitFailure
	}
	server := app.NewHTTPServer(cfg.Address(), handler)

	runCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	err = app.Run(runCtx, cfg.ShutdownTimeout, app.Dependencies{
		Server:            server,
		Listener:          listener,
		ShutdownTelemetry: observabilityRuntime.Shutdown,
		SyncLogger: func() error {
			return syncLogger(logger)
		},
	})
	if err == nil {
		return exitSuccess
	}

	logger.Error(runtimeFailureMessage, zap.Error(err))
	if syncErr := syncLogger(logger); syncErr != nil {
		writeFallback(os.Stderr, fmt.Errorf("sync logger after runtime failure: %w", syncErr))
	}
	return exitFailure
}

func cleanupRuntimeAfterStartupFailure(
	cfg config.Config,
	runtime *observability.Runtime,
	logger *zap.Logger,
) {
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := runtime.Shutdown(shutdownCtx); err != nil {
		logger.Error(telemetryCleanupFailureMessage, zap.Error(err))
	}
	if err := syncLogger(logger); err != nil {
		writeFallback(os.Stderr, fmt.Errorf("sync logger after startup failure: %w", err))
	}
}

func syncLogger(logger *zap.Logger) error {
	return normalizeStandardStreamSyncError(logger.Sync())
}

func normalizeStandardStreamSyncError(err error) error {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return err
	}
	if pathErr.Path != standardOutputPath && pathErr.Path != standardErrorPath {
		return err
	}
	if errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTTY) ||
		errors.Is(err, syscall.EBADF) {
		return nil
	}
	return err
}

func writeFallback(writer io.Writer, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	if len(message) > maximumFallbackErrorBytes {
		message = message[:maximumFallbackErrorBytes]
	}
	_, _ = fmt.Fprintf(writer, "%s%s\n", fallbackPrefix, message)
}
