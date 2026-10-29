package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

type headerCountingWriter struct {
	*httptest.ResponseRecorder
	statuses []int
}

func (w *headerCountingWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func TestRenderCommitsOnlySuccessfulHTML(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"success", nil, http.StatusCreated},
		{"template failure", errors.New("template failed"), http.StatusInternalServerError},
		{"request cancellation", context.Canceled, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.GET("/", func(c echo.Context) error {
				return render(c, http.StatusCreated, templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
					if _, err := io.WriteString(w, "<p>Rendered page</p>"); err != nil {
						return err
					}
					return tc.err
				}))
			})
			w := &headerCountingWriter{ResponseRecorder: httptest.NewRecorder()}
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if len(w.statuses) != 1 || w.statuses[0] != tc.status || w.Code != tc.status {
				t.Fatalf("response committed incorrectly: headers=%v status=%d", w.statuses, w.Code)
			}
			if tc.err == nil {
				if w.Body.String() != "<p>Rendered page</p>" || w.Header().Get("Content-Type") != echo.MIMETextHTMLCharsetUTF8 {
					t.Fatal("successful HTML response lost its body or content type")
				}
			} else if strings.Contains(w.Body.String(), "Rendered page") || !strings.HasPrefix(w.Header().Get("Content-Type"), echo.MIMEApplicationJSON) {
				t.Fatalf("failed render leaked partial HTML or the wrong content type: %s %s", w.Header().Get("Content-Type"), w.Body.String())
			}
		})
	}
}

func TestRenderReusesOnlyCleanBoundedBuffers(t *testing.T) {
	e := echo.New()
	serve := func(t templ.Component) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		_ = render(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), w), http.StatusOK, t)
		return w
	}
	failed := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "<p>partial</p>")
		return errors.New("template failed")
	})
	page := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<p>page</p>")
		return err
	})
	for range 8 {
		if w := serve(failed); w.Body.Len() != 0 {
			t.Fatalf("failed render wrote %q", w.Body.String())
		}
		if w := serve(page); w.Body.String() != "<p>page</p>" {
			t.Fatalf("reused buffer leaked earlier output: %q", w.Body.String())
		}
	}
	huge := strings.Repeat("x", maxPooledRenderBuffer+1)
	if w := serve(templ.Raw(huge)); w.Body.Len() != len(huge) {
		t.Fatal("oversized page was truncated")
	}
	for range 64 {
		buffer := renderBuffers.Get().(*bytes.Buffer)
		if buffer.Cap() > maxPooledRenderBuffer || buffer.Len() != 0 {
			t.Fatalf("pool returned an oversized or dirty buffer: len=%d cap=%d", buffer.Len(), buffer.Cap())
		}
	}
}

type discardResponse struct{ header http.Header }

func (w *discardResponse) Header() http.Header       { return w.header }
func (*discardResponse) Write(p []byte) (int, error) { return len(p), nil }
func (*discardResponse) WriteHeader(int)             {}

// BenchmarkRenderIndex measures one full results page through render, so the
// output buffer's growth and reuse dominate the allocation profile.
func BenchmarkRenderIndex(b *testing.B) {
	start := time.Date(2026, 11, 14, 19, 30, 0, 0, time.UTC)
	page := home.SearchPage{ReturnURL: "/"}
	for i := range 20 {
		page.Events.Items = append(page.Events.Items, models.Event{
			ID: fmt.Sprintf("ticketmaster:bench-%d", i), Name: fmt.Sprintf("Benchmark concert %d", i), Status: "onsale",
			Source: models.Source{Provider: "ticketmaster", ID: fmt.Sprintf("bench-%d", i), URL: "https://example.com/event"},
			Start:  models.EventStart{DateTime: &start},
		})
	}
	page.Events.Total = len(page.Events.Items)
	e := echo.New()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		c := e.NewContext(request, &discardResponse{header: http.Header{}})
		if err := render(c, http.StatusOK, home.Index(page)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRenderTracksEchoResponse(t *testing.T) {
	w := &headerCountingWriter{ResponseRecorder: httptest.NewRecorder()}
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), w)
	err := render(c, http.StatusOK, templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "HTML")
		return err
	}))
	if err != nil || !c.Response().Committed || c.Response().Status != http.StatusOK || c.Response().Size != 4 {
		t.Fatalf("HTML bypassed Echo response tracking: %+v, %v", c.Response(), err)
	}
	c.Error(errors.New("late error"))
	if len(w.statuses) != 1 || w.Body.String() != "HTML" {
		t.Fatal("late error rewrote an already committed response")
	}
}
