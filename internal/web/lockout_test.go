package web

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// through reports a request that a tunnel handed over from loopback for the given client address.
func through(client string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = "127.0.0.1:40000"
		r.Header.Set("X-Forwarded-For", client)
	}
}

func TestLoginLockoutSurvivesAddressRotation(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	login := func(pw, client string) int {
		t.Helper()
		return e.do("POST", "/api/login", `{"password":"`+pw+`"}`, through(client)).Code
	}
	// a new address from one IPv6 /64 for every guess
	for i := 1; i <= 5; i++ {
		if code := login("wrong-guess", "2001:db8::"+strconv.Itoa(i)); code != http.StatusUnauthorized {
			t.Fatalf("guess %d: %d", i, code)
		}
	}
	if code := login("wrong-guess", "2001:db8::6"); code != http.StatusTooManyRequests {
		t.Fatalf("6th guess from the same /64: %d, want 429", code)
	}
	// a fresh IPv4 address for every guess: the internet as a whole gets 20 failures per 15 minutes
	for i := 1; i <= 15; i++ {
		if code := login("wrong-guess", "203.0.113."+strconv.Itoa(i)); code != http.StatusUnauthorized {
			t.Fatalf("internet guess %d: %d", i, code)
		}
	}
	if code := login("correct-horse", "198.51.100.1"); code != http.StatusTooManyRequests {
		t.Fatalf("21st internet sign-in: %d, want 429", code)
	}
	// Tailscale and the home network still get in
	if code := login("correct-horse", "100.70.24.39"); code != http.StatusOK {
		t.Fatalf("tailnet sign-in during an internet lockout: %d", code)
	}
	if code := e.do("POST", "/api/login", `{"password":"correct-horse"}`).Code; code != http.StatusOK {
		t.Fatalf("LAN sign-in during an internet lockout: %d", code)
	}
	e.now = e.now.Add(15*time.Minute + time.Second)
	if code := login("correct-horse", "198.51.100.1"); code != http.StatusOK {
		t.Fatalf("internet sign-in after the lockout: %d", code)
	}
}
