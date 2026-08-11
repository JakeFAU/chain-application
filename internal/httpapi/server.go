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

func NewHandler(server StrictServerInterface, wrap func(http.Handler) http.Handler) http.Handler {
	router := chi.NewRouter()
	strictHandler := HandlerFromMux(
		NewStrictHandlerWithOptions(server, nil, strictHTTPServerOptions()),
		router,
	)
	return wrap(strictHandler)
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
