package kgraph

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

//go:embed ui.html
var uiHTML []byte

// mountUI serves the single-page console at /. Embedded and self-contained: no
// CDN, no build step, no second toolchain to keep working. The force layout is
// thirty lines of vanilla JS for the same reason.
//
// There is no /verify here. Diarization and transcript verification moved to
// `oidio verify`, which owns the audio stack; kgraph consumes the rendered
// transcript as a source document and needs to know nothing about speakers.
func mountUI(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(uiHTML)
	})
}

// mountEvents serves the change stream at /events. Not a huma operation: SSE is
// a long-lived stream, not a response body, and modelling it as one would fight
// the framework for nothing.
func mountEvents(mux *http.ServeMux, reg *Registry) {
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		if dir == "" {
			http.Error(w, "dir is required", http.StatusBadRequest)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		ch, cancel, err := reg.Watch(dir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer cancel()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no") // defeat proxy buffering
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "retry: 2000\n\n")
		flusher.Flush()

		// A comment every 20s keeps the connection alive through anything that
		// times out an idle socket, and lets the client notice a dead daemon.
		beat := time.NewTicker(20 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-beat.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			case c, open := <-ch:
				if !open {
					return
				}
				b, err := json.Marshal(c)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: change\ndata: %s\n\n", b)
				flusher.Flush()
			}
		}
	})
}

// isLoopback reports whether an address binds only to this machine.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "", "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// lanHint finds a routable address to print, so the LAN URL does not have to be
// looked up by hand.
func lanHint(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range ifaces {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.To4() == nil {
			continue
		}
		return "http://" + net.JoinHostPort(n.IP.String(), port)
	}
	return ""
}
