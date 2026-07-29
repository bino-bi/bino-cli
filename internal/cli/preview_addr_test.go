package cli

import "testing"

// Preview auto-opens a browser at server.URL(), which is the literal bind. With
// --addr that can be a wildcard, and a wildcard URL is both useless to a browser
// and a strong hint that nobody is sitting in front of one (the container case).
func TestIsLoopbackURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:45678", true},
		{"http://localhost:45678", true},
		{"http://[::1]:45678", true},
		{"http://0.0.0.0:8080", false},
		{"http://[::]:8080", false}, // what server.URL() reports for --addr 0.0.0.0:8080
		{"http://192.168.0.106:8080", false},
		{"", false},
		{"://nonsense", false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			if got := isLoopbackURL(tc.url); got != tc.want {
				t.Errorf("isLoopbackURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
