package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4/middleware"
	"net/http"

	"github.com/labstack/echo/v4"

	"hw1/internal/handler"
)

type Server struct {
	*echo.Echo
	handler *handler.Handler
}

func New(handler *handler.Handler) *Server {
	return &Server{Echo: echo.New(), handler: handler}
}

func (s *Server) Start(addr string) error {
	s.Use(middleware.Recover())
	s.Use(middleware.RequestID())

	InitRoutes(s)

	if err := s.Echo.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("start %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.Echo.Shutdown(ctx)
}
