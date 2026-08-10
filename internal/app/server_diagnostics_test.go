package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JakeFAU/chain-application/internal/app"
	"github.com/JakeFAU/chain-application/internal/config"
	"github.com/JakeFAU/chain-application/internal/observability"
	"go.uber.org/zap/zapcore"
)

const (
	panicSentinel               = "private-panic-diagnostic-1f15ed4d"
	httpServerDiagnosticMessage = "HTTP server diagnostic"
	serverTestTimeout           = 3 * time.Second
)

func TestHTTPServerPanicDiagnosticIsStaticAndDoesNotUseGlobalLogger(t *testing.T) {
	var zapOutput bytes.Buffer
	cfg, err := config.Load(func(string) (string, bool) { return "", false }, "test-version")
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	runtime, err := observability.New(
		t.Context(),
		cfg,
		observability.WithLogSink(zapcore.AddSync(&zapOutput)),
	)
	if err != nil {
		t.Fatalf("construct observability: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown observability: %v", err)
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := app.NewHTTPServer(
		listener.Addr().String(),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(panicSentinel) }),
		runtime.HTTPServerErrorLog(),
	)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	var globalLog bytes.Buffer
	previousGlobalWriter := log.Writer()
	log.SetOutput(&globalLog)
	t.Cleanup(func() { log.SetOutput(previousGlobalWriter) })

	requestCtx, cancelRequest := context.WithTimeout(t.Context(), serverTestTimeout)
	defer cancelRequest()
	request, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodGet,
		"http://"+listener.Addr().String()+"/panic",
		http.NoBody,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, requestErr := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if requestErr == nil {
		t.Fatal("panic request error = nil, want connection failure")
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	select {
	case err := <-serveResult:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve error = %v, want http.ErrServerClosed", err)
		}
	case <-requestCtx.Done():
		t.Fatalf("timed out waiting for Serve: %v", requestCtx.Err())
	}

	if output := globalLog.String(); output != "" {
		t.Fatalf("global standard log = %q, want empty", output)
	}
	if strings.Contains(zapOutput.String(), panicSentinel) {
		t.Fatalf("Zap output disclosed panic sentinel: %q", zapOutput.String())
	}

	lines := strings.Split(strings.TrimSpace(zapOutput.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("Zap entries = %d, want 1: %q", len(lines), zapOutput.String())
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("decode Zap entry: %v", err)
	}
	if entry["message"] != httpServerDiagnosticMessage {
		t.Fatalf("message = %v, want %q", entry["message"], httpServerDiagnosticMessage)
	}
}
