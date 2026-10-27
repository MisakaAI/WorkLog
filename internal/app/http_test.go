package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"worklog/internal/transport/rest"
)

const httpTestTimeout = 10 * time.Second

func testHTTP(t *testing.T, addr string, handler http.Handler) *HTTP {
	t.Helper()
	h := NewHTTP(addr, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), httpTestTimeout)
		defer cancel()
		_ = h.Shutdown(ctx)
	})
	return h
}

func receiveHTTP[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(httpTestTimeout):
		t.Fatal("timed out waiting for HTTP lifecycle event")
		var zero T
		return zero
	}
}

func assertHTTPBlocked[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case value := <-ch:
		t.Fatalf("operation returned before handler exited: %v", value)
	default:
	}
}

func assertHTTPNormalDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err, ok := <-done:
		if ok || err != nil {
			t.Fatalf("normal shutdown reported an error value: %v, open=%v", err, ok)
		}
	case <-time.After(httpTestTimeout):
		t.Fatal("normal shutdown did not close Done")
	}
}

func fetchHTTP(url string) (string, error) {
	client := &http.Client{
		Timeout: httpTestTimeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err == nil && response.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP status %d: %s", response.StatusCode, body)
	}
	return string(body), err
}

func TestHTTPRestartRetainsPortHandlerAndServerDefaults(t *testing.T) {
	var requests atomic.Int32
	h := testHTTP(t, "127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, requests.Add(1))
	}))
	if h.URL() != "" || h.Done() != nil {
		t.Fatal("new HTTP should have no URL or generation")
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	var firstURL string
	var previousDone <-chan error
	for i := 1; i <= 3; i++ {
		if err := h.Start(); err != nil {
			t.Fatal(err)
		}
		url, done := h.URL(), h.Done()
		if !strings.HasPrefix(url, "http://127.0.0.1:") || strings.HasSuffix(url, ":0") {
			t.Fatalf("not an actual bound URL: %q", url)
		}
		if firstURL == "" {
			firstURL = url
		}
		if url != firstURL {
			t.Fatalf("restart changed address: %q != %q", url, firstURL)
		}
		if done == nil || done == previousDone {
			t.Fatal("successful restart must create a fresh generation")
		}
		if previousDone != nil {
			assertHTTPNormalDone(t, previousDone)
		}
		if err := h.Start(); err != nil || h.Done() != done || h.URL() != url {
			t.Fatalf("duplicate Start changed generation: %v", err)
		}

		// Check that the shared helper's transport limits are preserved.
		got, want := h.current.server, rest.HTTPServer(h.current.listener.Addr().String(), nil)
		if got.Addr != want.Addr || got.ReadHeaderTimeout != want.ReadHeaderTimeout ||
			got.ReadTimeout != want.ReadTimeout || got.WriteTimeout != want.WriteTimeout ||
			got.IdleTimeout != want.IdleTimeout || got.MaxHeaderBytes != want.MaxHeaderBytes {
			t.Fatal("server does not retain rest.HTTPServer settings")
		}
		body, err := fetchHTTP(url)
		if err != nil || body != fmt.Sprint(i) {
			t.Fatalf("handler was not reused: body=%q, err=%v", body, err)
		}
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if h.URL() != "" || h.Done() != done {
			t.Fatal("stopped server should clear URL and retain generation signal")
		}
		assertHTTPNormalDone(t, done)
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatalf("repeated Shutdown: %v", err)
		}
		previousDone = done
	}
}

func TestHTTPBindFailureCanRetryAndRetainsChosenAddress(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	h := testHTTP(t, occupied.Addr().String(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err := h.Start(); err == nil {
		t.Fatal("Start succeeded on an occupied address")
	}
	if h.URL() != "" || h.Done() != nil {
		t.Fatal("failed initial bind published a URL or generation")
	}
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("retry after bind failure: %v", err)
	}
	url, done := h.URL(), h.Done()
	addr := strings.TrimPrefix(url, "http://")
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocker, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if err := h.Start(); err == nil {
		t.Fatal("restart silently switched to a different port")
	}
	if h.URL() != "" || h.Done() != done {
		t.Fatal("failed restart changed URL or previous generation")
	}
	if err := blocker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if h.URL() != url || h.Done() == done {
		t.Fatal("retry did not retain address and create a new generation")
	}
}

func TestHTTPShutdownGracefullyDrainsAndSerializesRestart(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	h := testHTTP(t, "127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		if r.Context().Err() != nil {
			t.Error("graceful shutdown canceled the active request")
		}
		_, _ = io.WriteString(w, "drained")
	}))
	defer unblock()
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	url, done := h.URL(), h.Done()
	request := make(chan error, 1)
	go func() {
		body, err := fetchHTTP(url)
		if err == nil && body != "drained" {
			err = fmt.Errorf("response body = %q", body)
		}
		request <- err
	}()
	receiveHTTP(t, entered)
	shutdown := make(chan error, 1)
	go func() { shutdown <- h.Shutdown(context.Background()) }()
	assertHTTPNormalDone(t, done) // Serve stops before the handler finishes.
	if h.URL() != "" {
		t.Fatal("URL must be blank while shutdown drains")
	}
	assertHTTPBlocked(t, shutdown)

	started := make(chan struct{})
	restart := make(chan error, 1)
	go func() {
		close(started)
		restart <- h.Start()
	}()
	receiveHTTP(t, started)
	assertHTTPBlocked(t, restart)
	unblock()
	if err := receiveHTTP(t, shutdown); err != nil {
		t.Fatal(err)
	}
	if err := receiveHTTP(t, request); err != nil {
		t.Fatal(err)
	}
	if err := receiveHTTP(t, restart); err != nil {
		t.Fatal(err)
	}
	if h.URL() != url || h.Done() == done {
		t.Fatal("serialized restart did not create a fresh generation at the same URL")
	}
}

func TestHTTPShutdownForceClosesButWaitsForHandler(t *testing.T) {
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var finished atomic.Bool
	h := testHTTP(t, "127.0.0.1:0", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-release // Simulate DB work continuing after connection cancellation.
		finished.Store(true)
	}))
	defer unblock()
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	url, done := h.URL(), h.Done()
	request := make(chan error, 1)
	go func() { _, err := fetchHTTP(url); request <- err }()
	receiveHTTP(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- h.Shutdown(ctx) }()
	receiveHTTP(t, canceled) // Close fallback must cancel the connection context.
	assertHTTPNormalDone(t, done)
	assertHTTPBlocked(t, shutdown)
	if finished.Load() {
		t.Fatal("handler finished before its DB work was released")
	}
	if err := receiveHTTP(t, request); err == nil {
		t.Fatal("force-closed connection unexpectedly completed normally")
	}
	unblock()
	if err := receiveHTTP(t, shutdown); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown lost context error: %v", err)
	}
	if !finished.Load() {
		t.Fatal("Shutdown returned while handler work was still active")
	}
	if err := h.Shutdown(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("idempotent Shutdown lost original result: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("previous shutdown error prevented restart: %v", err)
	}
	if h.URL() != url || h.Done() == done {
		t.Fatal("forced shutdown did not permit a new generation on the same port")
	}
}

func TestHTTPUnexpectedServeFailureAndRetry(t *testing.T) {
	var calls atomic.Int32
	h := testHTTP(t, "127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	url, done, generation := h.URL(), h.Done(), h.current
	// Ensure Serve has accepted a request before sabotaging its listener.
	if _, err := fetchHTTP(url); err != nil {
		t.Fatal(err)
	}
	if err := generation.listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receiveHTTP(t, done); err == nil || errors.Is(err, http.ErrServerClosed) || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("unexpected Serve failure was not reported: %v", err)
	}
	assertHTTPNormalDone(t, done) // Exactly one error, then closed.
	if h.URL() != "" {
		t.Fatal("Serve failure left a stale URL")
	}

	// A connection goroutine scheduled late must never Add after the drain
	// starts, nor invoke the application handler. Exercise the admission gate.
	response := httptest.NewRecorder()
	generation.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("stopped generation admitted another handler")
	}
	if err := h.Start(); err != nil {
		t.Fatalf("retry after Serve failure: %v", err)
	}
	if h.URL() != url || h.Done() == done {
		t.Fatal("Serve failure retry lost the chosen address or generation isolation")
	}
	if body, err := fetchHTTP(url); err != nil || body != "ok" || calls.Load() != 2 {
		t.Fatalf("retry did not reuse handler: body=%q, err=%v", body, err)
	}
	assertHTTPNormalDone(t, done) // Restart never mutates the captured old signal.
}

func TestHTTPConcurrentLifecycleIsIdempotent(t *testing.T) {
	h := testHTTP(t, "127.0.0.1:0", http.NotFoundHandler())
	const workers = 16
	run := func(fn func() error) {
		t.Helper()
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				if err := fn(); err != nil {
					t.Errorf("concurrent lifecycle operation: %v", err)
				}
			})
		}
		wg.Wait()
	}
	run(h.Start)
	url, done := h.URL(), h.Done()
	if url == "" || done == nil {
		t.Fatal("concurrent starts did not establish a generation")
	}
	run(func() error {
		if err := h.Start(); err != nil {
			return err
		}
		if h.URL() != url || h.Done() != done {
			return errors.New("duplicate Start replaced a running generation")
		}
		return nil
	})
	run(func() error { return h.Shutdown(context.Background()) })
	assertHTTPNormalDone(t, done)
	if h.URL() != "" {
		t.Fatal("concurrent shutdowns left server running")
	}

	// Mix immediate starts/stops (including Shutdown racing Serve startup),
	// while reading snapshots. The race detector also checks state publication.
	run(func() error {
		for range 12 {
			if err := h.Start(); err != nil {
				return err
			}
			_ = h.URL()
			_ = h.Done()
			if err := h.Shutdown(context.Background()); err != nil {
				return err
			}
		}
		return nil
	})
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if h.URL() != url {
		t.Fatal("concurrent restarts changed the bound port")
	}
}

func TestHTTPNilDefaults(t *testing.T) {
	h := NewHTTP("127.0.0.1:0", nil, nil)
	if h.handler != http.DefaultServeMux || h.logger != slog.Default() {
		t.Fatal("nil handler/logger did not use standard defaults")
	}
}
