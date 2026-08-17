package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct{}

const (
	healthStatus               = "ok"
	invalidRequestMessage      = "invalid request"
	internalServerErrorMessage = "internal server error"
)

var _ StrictServerInterface = (*Server)(nil)

func (*Server) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: healthStatus}, nil
}

// NewHandler builds the routed strict API handler and applies wrap once at the
// outside. A nil wrap means no outer instrumentation and is not an error.
func NewHandler(server StrictServerInterface, wrap func(http.Handler) http.Handler) http.Handler {
	router := chi.NewRouter()
	strictHandler := HandlerWithOptions(
		NewStrictHandlerWithOptions(server, nil, strictHTTPServerOptions()),
		chiServerOptions(router),
	)
	if wrap == nil {
		return strictHandler
	}
	return wrap(strictHandler)
}

func chiServerOptions(router chi.Router) ChiServerOptions {
	return ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, invalidRequestMessage, http.StatusBadRequest)
		},
	}
}

func strictHTTPServerOptions() StrictHTTPServerOptions {
	return StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, invalidRequestMessage, http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, internalServerErrorMessage, http.StatusInternalServerError)
		},
	}
}
