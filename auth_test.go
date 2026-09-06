package kgraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve exercises the real middleware with a chosen peer address, which is the
// only thing that decides whether a token is needed.
func serve(t *testing.T, peer, header, cookie, query string) *httptest.ResponseRecorder {
	t.Helper()
	h := requireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	}), "goodtoken")
	url := "/attach"
	if query != "" {
		url += "?token=" + query
	}
	r := httptest.NewRequest(http.MethodPost, url, nil)
	r.RemoteAddr = peer
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: tokenCookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// The gate is on the PEER, not the listen address. Binding 0.0.0.0 to reach the
// console from a phone must not start demanding a token from the local MCP shim
// and every local `kg` call — a local caller can already run `kg` directly.
func TestLoopbackPeersNeedNoToken(t *testing.T) {
	for _, peer := range []string{"127.0.0.1:5555", "[::1]:5555", "127.0.0.53:9"} {
		if got := serve(t, peer, "", "", "").Code; got != http.StatusOK {
			t.Errorf("%s is loopback and was refused (%d)", peer, got)
		}
	}
}

// This is the whole point: /attach writes files, and warning about it was not
// enough because a warning is advice.
func TestOffBoxPeerIsRefusedWithoutAToken(t *testing.T) {
	w := serve(t, "192.168.1.50:5555", "", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("an off-box peer reached a write endpoint unauthenticated (%d)", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("want a Bearer challenge, got %q", got)
	}
	// The message has to say where the token is, or the workaround is turning the
	// guard off.
	if !strings.Contains(w.Body.String(), tokenRel) {
		t.Errorf("the refusal does not say where to find the token: %s", w.Body.String())
	}
}

func TestOffBoxPeerAcceptedWithTheToken(t *testing.T) {
	cases := []struct {
		name, header, cookie, query string
		want                        int
	}{
		{"bearer", "Bearer goodtoken", "", "", http.StatusOK},
		{"cookie", "", "goodtoken", "", http.StatusOK},
		{"query bootstraps a cookie", "", "", "goodtoken", http.StatusSeeOther},
		{"wrong bearer", "Bearer nope", "", "", http.StatusUnauthorized},
		{"wrong cookie", "", "nope", "", http.StatusUnauthorized},
		{"wrong query", "", "", "nope", http.StatusUnauthorized},
		{"bare token, no scheme", "goodtoken", "", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := serve(t, "10.0.0.9:5555", c.header, c.cookie, c.query).Code; got != c.want {
				t.Errorf("want %d, got %d", c.want, got)
			}
		})
	}
}

// A browser cannot set a header on a plain navigation, so `?token=` is its way
// in — but the token must not then sit in the address bar, in history, or in a
// referrer on the next click.
func TestQueryTokenIsExchangedForACookieAndRemoved(t *testing.T) {
	w := serve(t, "10.0.0.9:5555", "", "", "goodtoken")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want a redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); strings.Contains(loc, "token") {
		t.Errorf("the token survived into the redirect target: %s", loc)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == tokenCookie && c.Value == "goodtoken" {
			found = true
			if !c.HttpOnly {
				t.Error("the token cookie must be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("no token cookie was set")
	}
}

// A token must be per-host and secret. The project directory is committed and
// Syncthing-synced, so a token there is a token on every machine.
func TestDaemonTokenIsStableAndNotInTheProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := DaemonToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 32 {
		t.Errorf("token is only %d chars", len(a))
	}
	b, err := DaemonToken()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("the token must not change between reads, or every open console breaks on restart")
	}
	if strings.Contains(tokenRel, "..") || strings.HasPrefix(tokenRel, "/") {
		t.Error("the token path must stay under the home directory")
	}
}
