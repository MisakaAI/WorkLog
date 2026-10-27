package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"worklog/internal/config"
	"worklog/internal/service"
	"worklog/internal/storage"
	"worklog/internal/transport/rest"
)

func TestHTTPRestartRetainsBusinessDataAndAuthentication(t *testing.T) {
	store, err := storage.Open(context.Background(), "sqlite", filepath.Join(t.TempDir(), "worklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	c := config.Config{User: "owner", Timezone: "Asia/Shanghai", Local: true,
		BasicUser: "worklog", BasicPassword: "password-123", AllowedHosts: []string{"127.0.0.1"}}
	handler := rest.NewWithStaticFS(service.New(store, time.UTC), c, logger,
		fstest.MapFS{"index.html": {Data: []byte("embedded-ui")}})
	h := testHTTP(t, "127.0.0.1:0", handler)
	// Drain before the database deferred Close, including test failures.
	defer func() { _ = h.Shutdown(context.Background()) }()
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	url := h.URL()
	client := &http.Client{Timeout: httpTestTimeout}
	defer client.CloseIdleConnections()
	request := func(method, path, body string, authenticated bool) (int, string) {
		t.Helper()
		r, err := http.NewRequest(method, url+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if authenticated {
			r.SetBasicAuth(c.BasicUser, c.BasicPassword)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(data)
	}
	if code, body := request(http.MethodPost, "/api/tasks", `{"title":"survives-http-restart","idempotencyKey":"desktop-restart-test"}`, true); code != 200 {
		t.Fatal(code, body)
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal("HTTP shutdown closed shared database", err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if h.URL() != url {
		t.Fatal("browser URL changed during in-process restart")
	}
	if code, body := request(http.MethodGet, "/api/tasks", "", true); code != 200 || !strings.Contains(body, "survives-http-restart") {
		t.Fatal("business data missing after restart", code, body)
	}
	for _, path := range []string{"/", "/api/tasks", "/config.js"} {
		if code, _ := request(http.MethodGet, path, "", false); code != 401 {
			t.Fatal("restart bypassed authentication", path, code)
		}
	}
	if code, body := request(http.MethodGet, "/", "", true); code != 200 || body != "embedded-ui" {
		t.Fatal("embedded UI unavailable after restart", code, body)
	}
}
