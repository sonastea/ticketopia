package ticketopia

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestShutdownDrainsBeforeResourceCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var resourceClosed atomic.Bool
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		if resourceClosed.Load() {
			t.Error("resources closed before in-flight HTTP completed")
		}
		_, _ = io.WriteString(w, "done")
	})}
	status := make(chan int, 1)
	go func() {
		defer resourceClosed.Store(true)
		status <- serve(ctx, srv, func() error { return srv.Serve(listener) }, zerolog.Nop())
	}()
	go func() {
		defer close(finished)
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("server did not accept request")
	}
	cancel()
	select {
	case <-status:
		t.Fatal("shutdown skipped draining")
	case <-time.After(25 * time.Millisecond):
	}
	if resourceClosed.Load() {
		t.Fatal("premature resource cleanup")
	}
	close(release)
	select {
	case got := <-status:
		if got != 0 {
			t.Fatalf("shutdown status: %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	<-finished
}

func TestInvalidPersistenceFailsBeforeHTTPStartup(t *testing.T) {
	t.Setenv("PERSISTENCE_MODE", "invalid")
	if status := Execute(t.Context()); status != 1 {
		t.Fatalf("invalid config status: %d", status)
	}
}
