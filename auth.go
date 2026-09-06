package kgraph

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Binding beyond loopback exposes a write API — `/attach` rewrites artifacts and
// seals managed blocks — to everything that can route to this host. On a LAN or a
// VPN that is other people's laptops, a phone, and anything on the guest network.
// Warning about it was not enough: a warning is advice, and the thing being
// advised against is one flag away.
//
// So a bearer token is REQUIRED beyond loopback, and loopback stays open. That
// split is deliberate: requiring a token on loopback would break the MCP shim and
// every local `kg` invocation for no gain — a local attacker can already run `kg`
// directly — and a guard that makes normal work annoying is one that gets turned
// off.
const tokenRel = ".local/state/kgraph/daemon-token"

// tokenCookie lets a browser hold the token after one URL paste. A console has no
// way to send an Authorization header on a plain navigation, and serving the token
// in the page would hand it to anyone who can load the page — which is exactly
// who this is meant to stop.
const tokenCookie = "kg_token"

// DaemonToken returns the token for this host, generating it on first use.
// 0600, under the user's home, never in the project: a project is committed and
// synced, and a token in Syncthing is a token on every machine.
func DaemonToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := statePath("daemon-token")
	_ = home
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// requireToken gates on the PEER, not on the listen address.
//
// Gating on the listen address looked equivalent and was not: binding 0.0.0.0 to
// reach the console from a phone would then demand a token from the local MCP
// shim and every local `kg` call too. Peer-based keeps local tooling working
// exactly as before while still requiring a token from anything off-box, which is
// the only thing being defended against. A local attacker can already run `kg`.
func requireToken(next http.Handler, token string) http.Handler {
	ok := func(got string) bool {
		// Constant time, so the failure mode is not a way to learn the token.
		return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peerIsLoopback(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found && ok(bearer) {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(tokenCookie); err == nil && ok(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		// `?token=` is the browser's way in, and it is exchanged for a cookie and
		// then redirected away, so the token does not sit in the address bar, in
		// history, or in a referrer header on the next click.
		if q := r.URL.Query().Get("token"); q != "" && ok(q) {
			http.SetCookie(w, &http.Cookie{
				Name: tokenCookie, Value: q, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
				// Persistent, not a session cookie. A console you have to
				// re-authenticate every time the browser restarts is one whose
				// token ends up pasted somewhere convenient and permanent.
				MaxAge: int((365 * 24 * time.Hour).Seconds()),
			})
			clean := *r.URL
			qs := clean.Query()
			qs.Del("token")
			clean.RawQuery = qs.Encode()
			http.Redirect(w, r, clean.RequestURI(), http.StatusSeeOther)
			return
		}
		// Name the path they wanted and where the token lives — but never the token
		// itself. This response goes to whoever asked, and whoever asked has not
		// authenticated.
		w.Header().Set("WWW-Authenticate", `Bearer realm="kgraph"`)
		http.Error(w, fmt.Sprintf(
			"kgraph requires a token from off-box callers.\n\n"+
				"In a browser, append the token to this URL:\n"+
				"  %s%stoken=<token>\n\n"+
				"It is exchanged for a cookie and dropped from the address bar, so this is "+
				"a one-time step.\n"+
				"Programmatically: Authorization: Bearer <token>\n\n"+
				"The token is in ~/%s\n", r.URL.RequestURI(), joiner(r.URL.RawQuery), tokenRel),
			http.StatusUnauthorized)
	})
}

// joiner picks `?` or `&` so the suggested URL is valid either way.
func joiner(rawQuery string) string {
	if rawQuery == "" {
		return "?"
	}
	return "&"
}

// peerIsLoopback reports whether the request came from this host.
func peerIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// exposureNotice is what the operator sees at startup. It prints the URL that
// bootstraps a browser session, because a token nobody can find is a token nobody
// uses — and the workaround for that is turning the guard off.
func exposureNotice(addr, token string) string {
	if isLoopback(addr) {
		return ""
	}
	var b strings.Builder
	b.WriteString("kg: listening beyond loopback on " + addr + ".\n")
	b.WriteString("    /attach writes files, so a token is required. Trusted LAN or VPN only.\n")
	host := addr
	if h := lanHint(addr); h != "" {
		host = strings.TrimPrefix(h, "http://")
	}
	fmt.Fprintf(&b, "    console: http://%s/?token=%s\n", host, token)
	b.WriteString("    token also in ~/" + tokenRel + "\n")
	return b.String()
}
