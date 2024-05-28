package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/views/home"
)

type api struct {
	logger zerolog.Logger
}

func NewAPI(ctx context.Context, logger zerolog.Logger) *api {
	return &api{
		logger: logger,
	}
}

func (a *api) Server(port int) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: a.Routes(),
	}
}

func (a *api) Routes() *echo.Echo {
	e := echo.New()

	e.GET("/", func(c echo.Context) error {
		return render(c, http.StatusOK, home.Index())
	})

	return e
}

func render(ctx echo.Context, status int, t templ.Component) error {
	ctx.Response().Writer.WriteHeader(status)

	err := t.Render(context.Background(), ctx.Response().Writer)
	if err != nil {
		return ctx.String(http.StatusInternalServerError, "failed to render response template")
	}

	return nil
}
