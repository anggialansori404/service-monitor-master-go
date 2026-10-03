package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestCheckWSOrigin(t *testing.T) {
	SetAllowedWSOrigins(nil)
	defer SetAllowedWSOrigins(nil)

	cases := []struct {
		name    string
		origin  string
		host    string
		allowed bool
	}{
		{"no origin (native agent client)", "", "master:8080", true},
		{"same origin", "http://master:8080", "master:8080", true},
		{"same origin https", "https://monitor.example.com", "monitor.example.com", true},
		{"cross origin not whitelisted", "https://evil.com", "monitor.example.com", false},
		{"malformed origin", "://bad", "monitor.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/ws/agent", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := checkWSOrigin(r); got != tc.allowed {
				t.Errorf("checkWSOrigin(origin=%q, host=%q) = %v, want %v", tc.origin, tc.host, got, tc.allowed)
			}
		})
	}
}

func TestCheckWSOriginWhitelist(t *testing.T) {
	SetAllowedWSOrigins([]string{"https://dashboard.example.com"})
	defer SetAllowedWSOrigins(nil)

	r := httptest.NewRequest("GET", "/api/v1/ws/agent", nil)
	r.Host = "monitor.example.com"
	r.Header.Set("Origin", "https://dashboard.example.com")
	if !checkWSOrigin(r) {
		t.Error("whitelisted origin should be allowed")
	}

	r2 := httptest.NewRequest("GET", "/api/v1/ws/agent", nil)
	r2.Host = "monitor.example.com"
	r2.Header.Set("Origin", "https://other.example.com")
	if checkWSOrigin(r2) {
		t.Error("non-whitelisted origin should be rejected")
	}
}
