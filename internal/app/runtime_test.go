package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const testTimeout = 3 * time.Second

func TestRunShutsDownHTTPBeforeTelemetryAndLogger(t *testing.T) {
	testCtx, stopTest := context.WithTimeout(context.Background(), testTimeout)
	defer stopTest()

	listener := newLoopbackListener(t)
	requestCompleted := make(chan struct{})
	var requestOnce sync.Once
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
		requestOnce.Do(func() { close(requestCompleted) })
	})}

	telemetryCalled := make(chan struct{})
	loggerSynced := make(chan struct{})
	callbackErrors := make(chan error, 2)
	dependencies := Dependencies{
		Server:   server,
		Listener: listener,
		ShutdownTelemetry: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				callbackErrors <- errors.New("telemetry received an expired context")
			}
			connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			if err == nil {
				_ = connection.Close()
				callbackErrors <- errors.New("telemetry ran while the HTTP listener still accepted connections")
			}
			close(telemetryCalled)
			return nil
		},
		SyncLogger: func() error {
			select {
			case <-telemetryCalled:
			default:
				callbackErrors <- errors.New("logger sync ran before telemetry shutdown")
			}
			close(loggerSynced)
			return nil
		},
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() {
		runResult <- Run(runCtx, 2*time.Second, dependencies)
	}()

	request, err := http.NewRequestWithContext(testCtx, http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(request)
	if err != nil {
		t.Fatalf("make request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	waitForSignal(t, testCtx, requestCompleted, "request completion")

	cancelRun()
	if err := waitForResult(t, testCtx, runResult, "Run return"); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	waitForSignal(t, testCtx, telemetryCalled, "telemetry shutdown")
	waitForSignal(t, testCtx, loggerSynced, "logger sync")

	select {
	case err := <-callbackErrors:
		t.Fatal(err)
	default:
	}

	request, err = http.NewRequestWithContext(testCtx, http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("create post-shutdown request: %v", err)
	}
	response, err = (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(request)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("request after Run returned succeeded, want listener closed")
	}
}

func TestRunTreatsHTTPServerClosedAsNormal(t *testing.T) {
	listener := newLoopbackListener(t)
	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()

	err := Run(runCtx, 2*time.Second, Dependencies{
		Server:            &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		Listener:          listener,
		ShutdownTelemetry: func(context.Context) error { return nil },
		SyncLogger:        func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRunReturnsServeError(t *testing.T) {
	serveFailure := errors.New("accept failed")

	err := Run(context.Background(), 2*time.Second, Dependencies{
		Server:            &http.Server{},
		Listener:          &failingListener{err: serveFailure},
		ShutdownTelemetry: func(context.Context) error { return nil },
		SyncLogger:        func() error { return nil },
	})
	if !errors.Is(err, serveFailure) {
		t.Fatalf("Run() error = %v, want serve failure", err)
	}
}

func TestRunFailsFastForInvalidInputs(t *testing.T) {
	valid := Dependencies{
		Server:            &http.Server{},
		Listener:          &failingListener{err: errors.New("unexpected serve")},
		ShutdownTelemetry: func(context.Context) error { return nil },
		SyncLogger:        func() error { return nil },
	}

	tests := []struct {
		name         string
		ctx          context.Context
		timeout      time.Duration
		dependencies Dependencies
	}{
		{name: "nil context", ctx: nil, timeout: 2 * time.Second, dependencies: valid},
		{name: "timeout does not preserve telemetry reserve", ctx: context.Background(), timeout: time.Second, dependencies: valid},
		{name: "nil server", ctx: context.Background(), timeout: 2 * time.Second, dependencies: withServer(valid, nil)},
		{name: "nil listener", ctx: context.Background(), timeout: 2 * time.Second, dependencies: withListener(valid, nil)},
		{name: "nil telemetry shutdown", ctx: context.Background(), timeout: 2 * time.Second, dependencies: withTelemetryShutdown(valid, nil)},
		{name: "nil logger sync", ctx: context.Background(), timeout: 2 * time.Second, dependencies: withLoggerSync(valid, nil)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Run(test.ctx, test.timeout, test.dependencies)
			if err == nil {
				t.Fatal("Run() error = nil, want validation error")
			}
			if strings.Contains(err.Error(), "unexpected serve") {
				t.Fatalf("Run() error = %v, validation did not fail before serving", err)
			}
		})
	}
}

func TestRunJoinsIndependentCleanupErrors(t *testing.T) {
	serveFailure := errors.New("serve failure")
	telemetryFailure := errors.New("telemetry failure")
	syncFailure := errors.New("sync failure")

	err := Run(context.Background(), 2*time.Second, Dependencies{
		Server:   &http.Server{},
		Listener: &failingListener{err: serveFailure},
		ShutdownTelemetry: func(context.Context) error {
			return telemetryFailure
		},
		SyncLogger: func() error {
			return syncFailure
		},
	})
	for _, expected := range []error{serveFailure, telemetryFailure, syncFailure} {
		if !errors.Is(err, expected) {
			t.Errorf("Run() error = %v, want joined error %v", err, expected)
		}
	}
}

func TestRunPreservesHTTPShutdownErrorAndTelemetryReserve(t *testing.T) {
	testCtx, stopTest := context.WithTimeout(context.Background(), testTimeout)
	defer stopTest()

	listener := newLoopbackListener(t)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		response.WriteHeader(http.StatusNoContent)
	})}
	telemetryCalled := make(chan struct{})
	telemetryFailure := errors.New("telemetry failure")
	syncFailure := errors.New("sync failure")

	runCtx, cancelRun := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() {
		runResult <- Run(runCtx, time.Second+50*time.Millisecond, Dependencies{
			Server:   server,
			Listener: listener,
			ShutdownTelemetry: func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					return errors.New("telemetry reserve expired before telemetry shutdown")
				}
				close(telemetryCalled)
				return telemetryFailure
			},
			SyncLogger: func() error { return syncFailure },
		})
	}()

	requestResult := make(chan error, 1)
	go func() {
		request, err := http.NewRequestWithContext(testCtx, http.MethodGet, "http://"+listener.Addr().String(), nil)
		if err != nil {
			requestResult <- err
			return
		}
		response, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(request)
		if err == nil {
			_ = response.Body.Close()
		}
		requestResult <- err
	}()

	waitForSignal(t, testCtx, requestStarted, "request start")
	cancelRun()
	err := waitForResult(t, testCtx, runResult, "Run return")
	close(releaseRequest)
	if requestErr := waitForResult(t, testCtx, requestResult, "request return"); requestErr != nil {
		t.Fatalf("request error = %v, want nil", requestErr)
	}

	waitForSignal(t, testCtx, telemetryCalled, "telemetry shutdown")
	for _, expected := range []error{context.DeadlineExceeded, telemetryFailure, syncFailure} {
		if !errors.Is(err, expected) {
			t.Errorf("Run() error = %v, want joined error %v", err, expected)
		}
	}
}

func newLoopbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func waitForSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", name, ctx.Err())
	}
}

func waitForResult(t *testing.T, ctx context.Context, result <-chan error, name string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", name, ctx.Err())
		return nil
	}
}

type failingListener struct {
	err error
}

func (listener *failingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (*failingListener) Close() error                       { return nil }
func (*failingListener) Addr() net.Addr                     { return testAddress("failing-listener") }

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }

func withServer(dependencies Dependencies, server *http.Server) Dependencies {
	dependencies.Server = server
	return dependencies
}

func withListener(dependencies Dependencies, listener net.Listener) Dependencies {
	dependencies.Listener = listener
	return dependencies
}

func withTelemetryShutdown(
	dependencies Dependencies,
	shutdown func(context.Context) error,
) Dependencies {
	dependencies.ShutdownTelemetry = shutdown
	return dependencies
}

func withLoggerSync(dependencies Dependencies, syncLogger func() error) Dependencies {
	dependencies.SyncLogger = syncLogger
	return dependencies
}
