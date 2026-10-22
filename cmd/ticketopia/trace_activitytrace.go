//go:build activitytrace

// Diagnostic builds only (go build -tags activitytrace): with
// TICKETOPIA_TRACE_DIR set, SIGUSR1 starts a Go execution trace and SIGUSR2
// stops it, as <dir>/<n>.trace (complete once renamed from .partial). The
// deployment fixture uses this to trace a replica during selected periods.
// Release images are built without the tag and contain no trace control.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/trace"
	"syscall"
)

func init() {
	dir := os.Getenv("TICKETOPIA_TRACE_DIR")
	if dir == "" {
		return
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		var file *os.File
		n := 0
		for s := range signals {
			switch {
			case s == syscall.SIGUSR1 && file == nil:
				n++
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.partial", n)))
				if err == nil {
					if err = trace.Start(f); err != nil {
						f.Close()
					} else {
						file = f
					}
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, "trace start:", err)
				}
			case s == syscall.SIGUSR2 && file != nil:
				trace.Stop()
				err := file.Close()
				if err == nil {
					err = os.Rename(file.Name(), filepath.Join(dir, fmt.Sprintf("%d.trace", n)))
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, "trace stop:", err)
				}
				file = nil
			}
		}
	}()
}
