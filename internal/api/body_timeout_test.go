package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
)

const testBodyTimeout = 150 * time.Millisecond

// bodyTimeoutServer runs the production server configuration, with only its
// read timeout shortened, and probe routes registered on the app's router.
func bodyTimeoutServer(t *testing.T, routes func(*echo.Echo)) *httptest.Server {
	t.Helper()
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	e := app.Routes()
	routes(e)
	server := httptest.NewUnstartedServer(nil)
	server.Config = app.Server(0)
	if server.Config.ReadTimeout <= 0 || server.Config.WriteTimeout != 0 {
		t.Fatalf("server must bound request reads and leave write deadlines to the stream: read=%s write=%s", server.Config.ReadTimeout, server.Config.WriteTimeout)
	}
	server.Config.ReadTimeout = testBodyTimeout
	server.Config.Handler = e
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestTricklingRequestBodyIsCutOff(t *testing.T) {
	readErr := make(chan error, 1)
	server := bodyTimeoutServer(t, func(e *echo.Echo) {
		e.POST("/probe", func(c echo.Context) error {
			_, err := io.ReadAll(c.Request().Body)
			readErr <- err
			if err != nil {
				return c.NoContent(http.StatusRequestTimeout)
			}
			return c.NoContent(http.StatusNoContent)
		})
	})
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = fmt.Fprintf(conn, "POST /probe HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	// Keep sending one byte at a time, well inside any per-read idle window,
	// so only a whole-body deadline can stop the request.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := conn.Write([]byte(" ")); err != nil {
					return
				}
			}
		}
	}()
	started := time.Now()
	select {
	case err := <-readErr:
		var timeout net.Error
		if err == nil || !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("trickling body read ended with %v, want a timeout", err)
		}
		if elapsed := time.Since(started); elapsed > 10*testBodyTimeout {
			t.Fatalf("body deadline fired after %s", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a trickling body held the handler past its deadline")
	}
}

func TestSlowBodyStallIsCutOff(t *testing.T) {
	readErr := make(chan error, 1)
	server := bodyTimeoutServer(t, func(e *echo.Echo) {
		e.POST("/probe", func(c echo.Context) error {
			_, err := io.ReadAll(c.Request().Body)
			readErr <- err
			return c.NoContent(http.StatusNoContent)
		})
	})
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Headers arrive promptly, then the declared body never does.
	if _, err = fmt.Fprintf(conn, "POST /probe HTTP/1.1\r\nHost: test\r\nContent-Length: 10\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("a stalled body was read as complete")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled body held the handler past its deadline")
	}
}

func TestCompletedBodyDoesNotLimitHandlerOrStream(t *testing.T) {
	server := bodyTimeoutServer(t, func(e *echo.Echo) {
		// The body arrives in time; the handler then outlives the body deadline,
		// which must not cancel its context through the connection's background read.
		e.POST("/probe", func(c echo.Context) error {
			data, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return err
			}
			select {
			case <-c.Request().Context().Done():
				return c.String(http.StatusInternalServerError, "cancelled")
			case <-time.After(3 * testBodyTimeout):
			}
			return c.String(http.StatusOK, string(data))
		})
		// A stream shaped like the live activity SSE route: no body, its own
		// write deadline per frame, and a lifetime far beyond the body deadline.
		e.GET("/stream", func(c echo.Context) error {
			response := c.Response()
			response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			controller := http.NewResponseController(response.Writer)
			for i := range 4 {
				if i > 0 {
					select {
					case <-c.Request().Context().Done():
						return nil
					case <-time.After(testBodyTimeout):
					}
				}
				if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(response, "event: tick\ndata: %d\n\n", i); err != nil {
					return err
				}
				if err := controller.Flush(); err != nil {
					return err
				}
			}
			return nil
		})
	})

	response, err := http.Post(server.URL+"/probe", "application/json", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("handler outliving the body deadline failed: %s %s", response.Status, body)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	frames := 0
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			frames++
		}
	}
	if frames != 4 {
		t.Fatalf("stream ended after %d of 4 frames: %v", frames, scanner.Err())
	}
}
