package rest

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"worklog/internal/config"
)

func assertPublicRuntimeConfig(t *testing.T, w *httptest.ResponseRecorder, timezone string) {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("invalid runtime config response", w.Code, w.Header(), w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("runtime config must not be cached")
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal("invalid runtime JSON", err)
	}
	want := map[string]any{"apiBaseUrl": "/api", "businessTimezone": timezone}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("runtime config must contain only public fields", got)
	}
}

func TestDiskRuntimeConfigIsGeneratedAndNotServerConfig(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config.json", "config.js", "server-config.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("private-placeholder"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := config.Config{Local: true, User: "private-user", Timezone: "America/New_York", StaticDir: dir,
		DSN: "private-dsn", TokenFile: "private-token-path", WriteToken: strings.Repeat("secret", 8),
		AllowedHosts: []string{"localhost"}}
	h := New(nil, c, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "http://localhost/config.json", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		// chi's GetHead middleware uses the GET handler; net/http suppresses HEAD bodies on the wire.
		assertPublicRuntimeConfig(t, w, c.Timezone)
	}
	for _, path := range []string{"/config.js", "/server-config.json", "/assets/config.json"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatal("private or obsolete config exposed", path, w.Code)
		}
	}
}
