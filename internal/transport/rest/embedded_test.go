package rest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"worklog/internal/config"
)

func TestEmbeddedResourcesKeepSecurityAndRuntimeConfig(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":       {Data: []byte("embedded-ui")},
		"assets/editor.js": {Data: []byte("embedded-worker")},
		"config.json":      {Data: []byte("wrong-placeholder")},
		"config.js":        {Data: []byte("obsolete-script")},
		"secret.db":        {Data: []byte("never-public")},
	}
	for _, password := range []string{"", "password-123"} {
		c := config.Config{Local: true, User: "owner", BasicUser: "worklog", BasicPassword: password,
			Timezone: "Asia/Shanghai", AllowedHosts: []string{"127.0.0.1"}}
		h := NewWithStaticFS(nil, c, slog.New(slog.NewJSONHandler(io.Discard, nil)), assets)
		request := func(path, host, origin string, authenticate bool) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
			r.RemoteAddr = "127.0.0.1:9999"
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			if authenticate {
				r.SetBasicAuth("worklog", password)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
		for _, path := range []string{"/", "/config.json"} {
			if password != "" && request(path, "127.0.0.1:43210", "", false).Code != 401 {
				t.Fatal("embedded UI/config bypassed Basic authentication")
			}
		}
		for _, path := range []string{"/", "/assets/editor.js", "/config.json"} {
			w := request(path, "127.0.0.1:43210", "http://127.0.0.1:43210", password != "")
			if w.Code != 200 {
				t.Fatal(path, w.Code, w.Body.String())
			}
			if path == "/config.json" {
				assertPublicRuntimeConfig(t, w, c.Timezone)
			}
		}
		for _, path := range []string{"/secret.db", "/config.js"} {
			if request(path, "127.0.0.1:43210", "", password != "").Code != 404 {
				t.Fatal("embedded path allowlist bypassed", path)
			}
		}
		for _, path := range []string{"/", "/config.json"} {
			if request(path, "evil.example", "", password != "").Code != 403 ||
				request(path, "127.0.0.1:43210", "http://evil.example", password != "").Code != 403 {
				t.Fatal("Host/Origin restrictions bypassed", path)
			}
		}
	}
}
