package daemon

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// NewToken returns a random token that clients must send as
// "Authorization: Bearer <token>". The daemon writes it to its port file.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate daemon token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// TokenClient returns an HTTP client that sends token on every request. An
// empty token (a port file from an older daemon) sends no header.
func TokenClient(token string) *http.Client {
	return &http.Client{Transport: tokenTransport{token: token}}
}

type tokenTransport struct{ token string }

func (t tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token == "" {
		return http.DefaultTransport.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// authMiddleware guards every daemon route. Any local process or browser page
// can reach a loopback port, so a request needs the token from the port file
// (readable only by the user). Browsers send an Origin header on cross-origin
// requests; the daemon's clients never do. On a loopback listener the Host
// must be a loopback name too, so a DNS-rebound page cannot reach the daemon.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.loopback && !loopbackHost(r.Host) {
			s.writeJSONError(w, http.StatusForbidden, errors.New("host not allowed"), "forbidden-host")
			return
		}
		if r.Header.Get("Origin") != "" {
			s.writeJSONError(w, http.StatusForbidden, errors.New("origin not allowed"), "forbidden-origin")
			return
		}
		if !validToken(r.Header.Get("Authorization"), s.token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bino daemon"`)
			s.writeJSONError(w, http.StatusUnauthorized, errors.New("missing or invalid daemon token (see .bino/daemon.json)"), "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validToken(header, token string) bool {
	got, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
