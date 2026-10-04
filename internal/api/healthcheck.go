package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Health probes only check this process, not optional caches or external APIs.
func healthHandler(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.String(http.StatusOK, "ok\n")
}

func (a *api) readinessHandler(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	select {
	case <-a.shutdown:
		return c.String(http.StatusServiceUnavailable, "not ready\n")
	default:
		return c.String(http.StatusOK, "ok\n")
	}
}
