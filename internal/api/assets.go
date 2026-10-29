package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/views/assets"
)

// assetHandler serves embedded files. A request carrying the file's current
// content version can never change, so browsers and the CDN may keep it for a
// year; any other request revalidates against the version ETag, so a deploy is
// never hidden behind a cache.
func assetHandler(files http.Handler) echo.HandlerFunc {
	return func(c echo.Context) error {
		h := c.Response().Header()
		policy := "no-cache"
		if v, ok := assets.Version(c.Param("*")); ok {
			h.Set("ETag", `"`+v+`"`)
			if c.QueryParam("v") == v {
				policy = "public, max-age=31536000, immutable"
			}
		}
		h.Set("Cache-Control", policy)
		files.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
