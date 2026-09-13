package auth

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	SetTrustedProxies([]string{"10.0.0.0/8"})
	mk := func(remote, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	if ip := ClientIP(mk("203.0.113.9:1", "1.2.3.4")); ip != "203.0.113.9" {
		t.Fatalf("untrusted peer must not spoof: %s", ip)
	}
	if ip := ClientIP(mk("10.0.0.5:1", "1.2.3.4, 10.0.0.7")); ip != "1.2.3.4" {
		t.Fatalf("rightmost untrusted hop expected: %s", ip)
	}
	if ip := ClientIP(mk("10.0.0.5:1", "")); ip != "10.0.0.5" {
		t.Fatalf("no xff: %s", ip)
	}
	if ip := ClientIP(mk("[::1]:1", "1.2.3.4")); ip != "::1" {
		t.Fatalf("::1 not trusted here: %s", ip)
	}
}
