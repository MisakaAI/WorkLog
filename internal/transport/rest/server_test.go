package rest

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"worklog/internal/config"
	"worklog/internal/diagnostics"
	"worklog/internal/transport/security"
)

func TestFailureAndPanicDiagnostics(t *testing.T) {
	for _, mode := range []string{"error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			s := &Server{Config: config.Config{Local: true, User: "owner", AllowedHosts: []string{"localhost"}}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), limiter: security.NewLimiter()}
			secret := "PRIVATE_BODY_TOKEN_DSN"
			handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "panic" {
					panic(secret)
				}
				s.fail(w, r, diagnostics.Wrap("read_dashboard", errors.Join(&pgconn.PgError{Code: "08006", Message: secret}, errors.New(secret))))
			}))
			r := httptest.NewRequest("GET", "http://localhost/api/dashboard", nil)
			r.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 500 {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.Contains(logs.String(), secret) || strings.Contains(w.Body.String(), secret) {
				t.Fatal("sensitive error leaked")
			}
			if !strings.Contains(logs.String(), w.Header().Get("X-Request-ID")) {
				t.Fatal("missing correlation")
			}
			if mode == "panic" && !strings.Contains(logs.String(), "server_test.go") {
				t.Fatal("missing stack")
			}
			if mode == "error" && !strings.Contains(logs.String(), "08006") {
				t.Fatal("missing driver code")
			}
		})
	}
}

func TestStaticNameWhitelist(t *testing.T) {
	for _, p := range []string{"/data/worklog.db", "/go.mod", "/assets/key.db", "/assets/../app.js", "/assets/.secret.js", "/assets\\escape.js"} {
		if _, ok := staticName(p); ok {
			t.Fatal(p)
		}
	}
}
