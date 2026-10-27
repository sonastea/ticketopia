package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/views/assets"
	"github.com/sonastea/ticketopia/views/home"
)

// offlineRoutes serve the offline page and the worker that caches it. Both are
// rendered once: the worker's bytes carry the page's hash, so a changed page
// changes the worker and browsers install it (and the new page) on their next
// update check.
func offlineRoutes(e *echo.Echo) {
	var page bytes.Buffer
	if err := home.Offline().Render(context.Background(), &page); err != nil {
		panic(err) // A static template without inputs cannot fail at runtime.
	}
	sum := sha256.Sum256(page.Bytes())
	worker := bytes.Replace(assets.ServiceWorker, []byte("__OFFLINE_VERSION__"), []byte(hex.EncodeToString(sum[:8])), 1)
	e.GET("/offline", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.Blob(http.StatusOK, echo.MIMETextHTMLCharsetUTF8, page.Bytes())
	})
	e.GET("/sw.js", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.Blob(http.StatusOK, "text/javascript; charset=utf-8", worker)
	})
}
