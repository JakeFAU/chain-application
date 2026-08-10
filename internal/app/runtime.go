package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const telemetryShutdownReserve = time.Second

var (
	errNilContext             = errors.New("run context is required")
	errInvalidShutdownTimeout = errors.New("shutdown timeout must exceed the telemetry reserve")
	errNilServer              = errors.New("HTTP server is required")
	errNilListener            = errors.New("HTTP listener is required")
	errNilTelemetryShutdown   = errors.New("telemetry shutdown is required")
	errNilLoggerSync          = errors.New("logger sync is required")
)

// Dependencies contains the process-owned resources managed by Run.
type Dependencies struct {
	Server            *http.Server
	Listener          net.Listener
	ShutdownTelemetry func(context.Context) error
	SyncLogger        func() error
}

// Run serves HTTP until cancellation or a server failure, then closes owned
// resources in HTTP, telemetry, and logger order within one bounded deadline.
func Run(ctx context.Context, shutdownTimeout time.Duration, dependencies Dependencies) error {
	if err := validateRunInputs(ctx, shutdownTimeout, dependencies); err != nil {
		return err
	}

	serveResults := make(chan error, 1)
	go func() {
		serveResults <- dependencies.Server.Serve(dependencies.Listener)
	}()

	var serveErr error
	serveReturned := false
	select {
	case serveErr = <-serveResults:
		serveReturned = true
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	httpShutdownCtx, cancelHTTPShutdown := context.WithTimeout(
		shutdownCtx,
		shutdownTimeout-telemetryShutdownReserve,
	)
	httpShutdownErr := dependencies.Server.Shutdown(httpShutdownCtx)
	cancelHTTPShutdown()

	if !serveReturned {
		select {
		case serveErr = <-serveResults:
		case <-shutdownCtx.Done():
			serveErr = fmt.Errorf("wait for HTTP server: %w", shutdownCtx.Err())
		}
	}

	telemetryShutdownErr := dependencies.ShutdownTelemetry(shutdownCtx)
	loggerSyncErr := dependencies.SyncLogger()

	return errors.Join(
		normalizeServeError(serveErr),
		wrapError("shut down HTTP server", httpShutdownErr),
		wrapError("shut down telemetry", telemetryShutdownErr),
		wrapError("sync logger", loggerSyncErr),
	)
}

func validateRunInputs(
	ctx context.Context,
	shutdownTimeout time.Duration,
	dependencies Dependencies,
) error {
	if ctx == nil {
		return errNilContext
	}
	if shutdownTimeout <= telemetryShutdownReserve {
		return errInvalidShutdownTimeout
	}
	if dependencies.Server == nil {
		return errNilServer
	}
	if dependencies.Listener == nil {
		return errNilListener
	}
	if dependencies.ShutdownTelemetry == nil {
		return errNilTelemetryShutdown
	}
	if dependencies.SyncLogger == nil {
		return errNilLoggerSync
	}
	return nil
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve HTTP: %w", err)
}

func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
