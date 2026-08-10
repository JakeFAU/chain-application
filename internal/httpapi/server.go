package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct{}

const healthStatus = "ok"

var _ StrictServerInterface = (*Server)(nil)

func (*Server) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: healthStatus}, nil
}

func NewHandler(server *Server, wrap func(http.Handler) http.Handler) http.Handler {
	router := chi.NewRouter()
	strictHandler := HandlerFromMux(NewStrictHandler(server, nil), router)
	return wrap(strictHandler)
}
