package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
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
