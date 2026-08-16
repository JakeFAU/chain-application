package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	wrappedRequests := 0
	handler := NewHandler(&Server{}, func(next http.Handler) http.Handler {
		wrapperCalls++
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			wrappedRequests++
			response.Header().Set(wrapperHeader, "applied")
			next.ServeHTTP(response, request)
		})
	})
	if wrapperCalls != 1 {
		t.Fatalf("wrapper construction calls = %d, want 1", wrapperCalls)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "matched route", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "unmatched route", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, http.NoBody))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Header().Get(wrapperHeader) != "applied" {
				t.Fatal("outer wrapper did not observe the request")
			}
		})
	}
	if wrappedRequests != len(tests) {
		t.Fatalf("wrapped requests = %d, want %d", wrappedRequests, len(tests))
	}
}

func TestStrictErrorResponsesDoNotExposeInternalErrors(t *testing.T) {
	t.Parallel()

	const sentinel = "private-strict-handler-error-7003b856"
	options := strictHTTPServerOptions()
	tests := []struct {
		name       string
		handle     func(http.ResponseWriter, *http.Request, error)
		wantStatus int
		wantBody   string
	}{
		{
			name:       "request",
			handle:     options.RequestErrorHandlerFunc,
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid request\n",
		},
		{
			name:       "response",
			handle:     options.ResponseErrorHandlerFunc,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal server error\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			test.handle(recorder, request, errors.New(sentinel))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Body.String() != test.wantBody {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), test.wantBody)
			}
			if strings.Contains(recorder.Body.String(), sentinel) {
				t.Fatalf("body disclosed sentinel: %q", recorder.Body.String())
			}
		})
	}
}

func TestNewHandlerUsesStaticStrictResponseError(t *testing.T) {
	t.Parallel()

	const sentinel = "private-handler-response-error-854f90be"
	handler := NewHandler(errorServer{err: errors.New(sentinel)}, identityWrapper)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() != "internal server error\n" {
		t.Fatalf("body = %q, want static internal error", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), sentinel) {
		t.Fatalf("body disclosed sentinel: %q", recorder.Body.String())
	}
}

type errorServer struct {
	err error
}

func (server errorServer) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return nil, server.err
}

func identityWrapper(handler http.Handler) http.Handler {
	return handler
}

func TestNewHandlerServesWithoutOuterWrapper(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler := NewHandler(&Server{}, nil)
	if handler == nil {
		t.Fatal("NewHandler(server, nil) = nil, want the unwrapped strict handler")
	}
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != healthStatus {
		t.Fatalf("status body = %q, want %q", response.Status, healthStatus)
	}
}
