package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Liveness checks only the process, never SQL, optional caches, or providers.
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
	}
	if a.ready != nil && a.ready(c.Request().Context()) != nil {
		return c.String(http.StatusServiceUnavailable, "not ready\n")
	}
	// Shutdown may have started while the dependency check was in flight.
	select {
	case <-a.shutdown:
		return c.String(http.StatusServiceUnavailable, "not ready\n")
	default:
		return c.String(http.StatusOK, "ok\n")
	}
}
