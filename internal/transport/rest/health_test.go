package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"worklog/internal/config"
	"worklog/internal/repository"
	"worklog/internal/service"
	"worklog/internal/transport/security"
)

type healthRepository struct {
	repository.Repository
	ping func(context.Context) error
}

func (r healthRepository) Ping(ctx context.Context) error { return r.ping(ctx) }

func TestHealthRoutes(t *testing.T) {
	for _, path := range []string{"/healthz", "/api/healthz"} {
		for _, healthy := range []bool{true, false} {
			t.Run(path+"/"+map[bool]string{true: "healthy", false: "unavailable"}[healthy], func(t *testing.T) {
				calls := 0
				repo := healthRepository{ping: func(ctx context.Context) error {
					calls++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 2*time.Second {
						t.Error("health ping must have a 2 second timeout")
					}
					if !healthy {
						return errors.New("private database connection details")
					}
					return nil
				}}
				cfg := config.Config{Local: true, User: "owner", AllowedHosts: []string{"localhost"}}
				h := New(&service.Service{Repo: repo}, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
				r := httptest.NewRequest("GET", "http://localhost"+path, nil)
				r.RemoteAddr = "127.0.0.1:1234"
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := http.StatusOK
				if !healthy {
					want = http.StatusServiceUnavailable
				}
				if w.Code != want || calls != 1 {
					t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
				}
				if healthy {
					var out map[string]bool
					if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !out["ok"] {
						t.Fatal(w.Body.String(), err)
					}
				}
				if w.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("health response must not be cached")
				}
			})
		}
	}
}

func TestHealthAuthenticationAndTrust(t *testing.T) {
	cfg := config.Config{User: "owner", BasicUser: "owner", BasicPassword: "secret", AllowedHosts: []string{"localhost"}}
	repo := healthRepository{ping: func(context.Context) error { return nil }}
	h := New(&service.Service{Repo: repo}, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, tc := range []struct {
		path, host, origin string
		auth               bool
		want               int
	}{
		{"/healthz", "localhost", "", false, 200},
		{"/api/healthz", "localhost", "", false, 401},
		{"/api/healthz", "localhost", "", true, 200},
		{"/healthz", "untrusted.example", "", false, 403},
		{"/api/healthz", "untrusted.example", "", true, 403},
		{"/api/healthz", "localhost", "https://untrusted.example", true, 403},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+tc.path, nil)
		if tc.auth {
			r.SetBasicAuth("owner", "secret")
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%+v: status=%d body=%s", tc, w.Code, w.Body.String())
		}
	}
}

func TestHealthUsesSeparateLimits(t *testing.T) {
	s := &Server{
		Config:      config.Config{Local: true, User: "owner", AllowedHosts: []string{"localhost"}},
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		limiter:     security.NewLimiter(),
		healthSlots: make(chan struct{}, 1),
		Service: &service.Service{Repo: healthRepository{ping: func(context.Context) error {
			return nil
		}}},
	}
	s.limiter.Max = 0
	h := s.middleware(http.HandlerFunc(s.health))
	for _, path := range []string{"/healthz", "/api/healthz", "/api/dashboard"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := http.StatusOK
		if path == "/api/dashboard" {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	s.healthSlots <- struct{}{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/healthz", nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "2" {
		t.Fatal(w.Code, w.Body.String())
	}
}
