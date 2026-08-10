package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzReturnsContractResponse(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	NewHandler(&Server{}, identityWrapper).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var response map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 1 {
		t.Fatalf("response members = %d, want 1", len(response))
	}

	status, ok := response["status"]
	if !ok {
		t.Fatal("response is missing status")
	}

	var value string
	if err := json.Unmarshal(status, &value); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if value != "ok" {
		t.Fatalf("status body = %q, want %q", value, "ok")
	}
}

func TestNewHandlerAppliesOneOuterWrapper(t *testing.T) {
	t.Parallel()

	const wrapperHeader = "X-Test-Outer-Wrapper"
	wrapperCalls := 0
	handler := NewHandler(&Server{}, func(next http.Handler) http.Handler {
		wrapperCalls++
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set(wrapperHeader, "applied")
			next.ServeHTTP(response, request)
		})
	})
	if wrapperCalls != 1 {
		t.Fatalf("wrapper construction calls = %d, want 1", wrapperCalls)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Header().Get(wrapperHeader) != "applied" {
		t.Fatal("outer wrapper did not observe the request")
	}
}

func identityWrapper(handler http.Handler) http.Handler {
	return handler
}
