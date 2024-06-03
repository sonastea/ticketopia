package api

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var KEY, ROOT_URL string
var SIZE = "100"

type api struct {
	logger zerolog.Logger
	redis  *redis.Client
}

func NewAPI(ctx context.Context, logger zerolog.Logger, redis *redis.Client) *api {
	KEY = os.Getenv("TICKETMASTER_KEY")
	ROOT_URL = "https://app.ticketmaster.com/discovery/v2/events.json"

	return &api{
		logger: logger,
		redis:  redis,
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

	e.GET("/", a.retrieveEventsHandler)

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
