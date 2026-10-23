package security

import (
	"net/http/httptest"
	"strings"
	"testing"
	"worklog/internal/config"
)

func TestBearerTransportCapabilities(t *testing.T) {
	c := config.Config{User: "owner", WriteToken: strings.Repeat("w", 32), ReadToken: strings.Repeat("r", 32)}
	r := httptest.NewRequest("DELETE", "http://localhost/api/tasks/t", nil)
	for _, allow := range []bool{false, true} {
		c.MCPDestructive = allow
		for _, mcp := range []bool{false, true} {
			r.Header.Set("Authorization", "bearer "+c.WriteToken)
			id, err := Authenticate(c, r, mcp)
			if err != nil || !id.Write || id.Destructive != (mcp && allow) {
				t.Fatalf("mcp=%v allow=%v: %+v %v", mcp, allow, id, err)
			}
			r.Header.Set("Authorization", "Bearer "+c.ReadToken)
			id, err = Authenticate(c, r, mcp)
			if err != nil || id.Write || id.Destructive {
				t.Fatal(id, err)
			}
		}
	}
}

func TestCrossSiteNavigationNotFetchOrWrite(t *testing.T) {
	c := config.Config{AllowedHosts: []string{"localhost"}}
	for _, tc := range []struct {
		method, mode, dest string
		allowed            bool
	}{
		{"GET", "navigate", "document", true}, {"HEAD", "navigate", "document", true},
		{"POST", "navigate", "document", false}, {"GET", "cors", "empty", false}, {"GET", "no-cors", "image", false},
	} {
		r := httptest.NewRequest(tc.method, "http://localhost/", nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", tc.mode)
		r.Header.Set("Sec-Fetch-Dest", tc.dest)
		if (Trusted(c, r) == nil) != tc.allowed {
			t.Fatal(tc)
		}
		r.Header.Set("Origin", "https://evil.example")
		if Trusted(c, r) == nil {
			t.Fatal("untrusted Origin accepted")
		}
	}
}
