package api

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

type api struct {
  routes *echo.Echo
}

func InitializeEcho(ctx context.Context) *echo.Echo {
  e := echo.New()

  e.GET("/", func(c echo.Context) error {
    return c.String(http.StatusOK, "Hello world!")
  })

  return e
}
