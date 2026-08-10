package app

import (
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerConfiguresAddressHandlerAndTimeouts(t *testing.T) {
	const address = ":18080"
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	server := NewHTTPServer(address, handler)

	if server.Addr != address {
		t.Errorf("Addr = %q, want %q", server.Addr, address)
	}
	if server.Handler == nil {
		t.Error("Handler = nil, want configured handler")
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %s, want 15s", server.ReadTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %s, want 30s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %s, want 1m", server.IdleTimeout)
	}
}
