package api

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/models"
)

var openAPIETag = representationETag("application/yaml", openAPI)

func representationETag(kind string, body []byte) string {
	h := sha256.New()
	h.Write([]byte("v1\x00" + kind + "\x00"))
	h.Write(body)
	return fmt.Sprintf(`W/"v1-%x"`, h.Sum(nil))
}

func (a *api) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func taxonomyCachePolicy(meta models.Freshness, now time.Time) string {
	if meta.Stale || meta.DataAsOf.IsZero() || meta.DataAsOf.After(now) || meta.FreshUntil.IsZero() || !meta.FreshUntil.After(now) {
		return "no-store"
	}
	seconds := int(min(10*time.Minute, meta.FreshUntil.Sub(now)) / time.Second)
	return fmt.Sprintf("public, max-age=%d, must-revalidate", seconds)
}

func eventCachePolicy(stale bool) string {
	if stale {
		return "no-store"
	}
	return "public, no-cache"
}

func (a *api) privatePublicRead(c echo.Context) bool {
	_, authorization := c.Request().Header[echo.HeaderAuthorization]
	_, err := c.Cookie(a.authConfig.CookieName("session"))
	return authorization || err == nil
}

// Only the explicitly eligible routes install this boundary. Errors retain their
// normal status/Retry-After and are never stored, including serialization errors.
func (a *api) publicCacheBoundary(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		policy := "no-store"
		if a.privatePublicRead(c) {
			policy = "private, no-store"
		}
		c.Response().Header().Set("Cache-Control", policy)
		return next(c)
	}
}

type serializedResponse struct {
	bytes.Buffer
	header http.Header
}

func (w *serializedResponse) Header() http.Header { return w.header }
func (w *serializedResponse) WriteHeader(int)     {}

// Use Echo's configured serializer and indentation behavior, buffering exactly
// once so the validator covers the complete bytes that a 200 would send.
func (a *api) publicJSON(c echo.Context, data any, policy string) error {
	w := &serializedResponse{header: make(http.Header)}
	original := c.Response()
	c.SetResponse(echo.NewResponse(w, c.Echo()))
	indent := ""
	if _, pretty := c.QueryParams()["pretty"]; pretty || c.Echo().Debug {
		indent = "  "
	}
	err := c.Echo().JSONSerializer.Serialize(c, data, indent)
	c.SetResponse(original)
	if err != nil {
		return err
	}
	return a.publicBytes(c, echo.MIMEApplicationJSON, w.Bytes(), "", policy)
}

// An empty tag is derived from kind and body, only when the response may carry it.
func (a *api) publicBytes(c echo.Context, kind string, body []byte, tag, policy string) error {
	if a.privatePublicRead(c) {
		policy = "private, no-store"
	}
	h := c.Response().Header()
	h.Set("Cache-Control", policy)
	if strings.Contains(policy, "no-store") {
		h.Del("ETag")
	} else {
		if tag == "" {
			tag = representationETag(kind, body)
		}
		h.Set("ETag", tag)
		if matchesIfNoneMatch(c.Request().Header.Values("If-None-Match"), tag) {
			return c.NoContent(http.StatusNotModified)
		}
	}
	return c.Blob(http.StatusOK, kind, body)
}

// Parse the entire field before accepting a match: a malformed suffix must not
// turn a valid prefix into a 304. Commas inside opaque tags are ordinary bytes.
func matchesIfNoneMatch(fields []string, current string) bool {
	s := strings.Trim(strings.Join(fields, ","), " \t")
	if s == "*" {
		return true
	}
	current = strings.TrimPrefix(current, "W/")
	matched := false
	for s != "" {
		s = strings.TrimPrefix(s, "W/")
		if len(s) == 0 || s[0] != '"' {
			return false
		}
		i := 1
		for i < len(s) && s[i] != '"' {
			if s[i] < 0x21 || s[i] == 0x7f {
				return false
			}
			i++
		}
		if i == len(s) {
			return false
		}
		matched = matched || s[:i+1] == current
		s = strings.TrimLeft(s[i+1:], " \t")
		if s == "" {
			return matched
		}
		if s[0] != ',' {
			return false
		}
		s = strings.TrimLeft(s[1:], " \t")
		if s == "" {
			return false
		}
	}
	return false
}
