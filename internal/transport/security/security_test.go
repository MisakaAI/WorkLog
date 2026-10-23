package security

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"worklog/internal/config"
)

func TestTrustedIdentity(t *testing.T) {
	c := config.Config{Local: true, User: "owner", WriteToken: strings.Repeat("w", 32), ReadToken: strings.Repeat("r", 32), BasicUser: "human", BasicPassword: "a-private-human-password", AllowedHosts: []string{"localhost"}, AllowedOrigins: []string{"https://worklog.example"}}
	r := httptest.NewRequest("POST", "http://localhost/api/dashboard", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	if _, e := Authenticate(c, r, true); e == nil {
		t.Fatal("anonymous MCP")
	}
	if _, e := Authenticate(c, r, false); e == nil {
		t.Fatal("Basic enabled but bypassed")
	}
	r.SetBasicAuth(c.BasicUser, c.BasicPassword)
	id, e := Authenticate(c, r, false)
	if e != nil || !id.Destructive {
		t.Fatal(id, e)
	}
	if _, e = Authenticate(c, r, true); e == nil {
		t.Fatal("MCP accepted human Basic auth")
	}
	r.Header.Set("Authorization", "Bearer "+c.ReadToken)
	id, e = Authenticate(c, r, true)
	if e != nil || id.Write || id.Destructive {
		t.Fatal(id, e)
	}
	r.Header.Set("Authorization", "Bearer "+c.WriteToken)
	id, e = Authenticate(c, r, true)
	if e != nil || !id.Write || id.Destructive {
		t.Fatal(id, e)
	}
	c.MCPDestructive = true
	id, e = Authenticate(c, r, true)
	if e != nil || !id.Destructive {
		t.Fatal(id, e)
	}
	r.Header.Del("Authorization")
	c.BasicPassword = ""
	id, e = Authenticate(c, r, false)
	if e != nil || id.User != "owner" {
		t.Fatal(id, e)
	}
	r.Host = "worklog.example"
	if _, e = Authenticate(c, r, false); e == nil {
		t.Fatal("public proxy Host bypassed authentication")
	}
	r.Host = "localhost"
	r.Header.Set("Authorization", c.WriteToken)
	if _, e = Authenticate(c, r, true); e == nil {
		t.Fatal("missing Bearer scheme")
	}
	r.Header.Del("Authorization")
	r.RemoteAddr = "198.51.100.2:1234"
	if _, e = Authenticate(c, r, false); e == nil {
		t.Fatal("non-loopback anonymous")
	}
	r.Host = "evil.example"
	if Trusted(c, r) == nil {
		t.Fatal("DNS rebinding")
	}
	r.Host = "localhost"
	r.Header.Set("Origin", "https://evil.example")
	if Trusted(c, r) == nil {
		t.Fatal("bad origin")
	}
	r.Header.Set("Origin", "https://worklog.example")
	if e = Trusted(c, r); e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Origin", "null")
	if Trusted(c, r) == nil {
		t.Fatal("opaque origin")
	}
	r.Header.Del("Origin")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if Trusted(c, r) == nil {
		t.Fatal("cross-site fetch")
	}
}
func TestLimiter(t *testing.T) {
	l := NewLimiter()
	l.Max = 2
	one, e := l.Acquire()
	if e != nil {
		t.Fatal(e)
	}
	defer one()
	two, e := l.Acquire()
	if e != nil {
		t.Fatal(e)
	}
	two()
	if _, e = l.Acquire(); e == nil {
		t.Fatal("rate limit")
	}
	l.Start = time.Now().Add(-2 * time.Minute)
	three, e := l.Acquire()
	if e != nil {
		t.Fatal(e)
	}
	three()
	l = NewLimiter()
	releases := []func(){}
	for range 32 {
		r, e := l.Acquire()
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, r)
	}
	if _, e = l.Acquire(); e == nil {
		t.Fatal("concurrency limit")
	}
	for _, r := range releases {
		r()
	}
}
