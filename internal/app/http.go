// Package app provides lifecycle components shared by application entry points.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"worklog/internal/transport/rest"
)

// HTTP owns a restartable HTTP listener, but not its handler's dependencies.
// Shutdown must finish before those dependencies (for example a DB) are closed.
// HTTP must not be copied after first use.
type HTTP struct {
	mu      sync.Mutex // serializes Start and the entire Shutdown, including draining
	addr    string
	handler http.Handler
	logger  *slog.Logger

	stateMu sync.RWMutex // permits URL and Done calls while Shutdown is draining
	current *httpGeneration
	url     string
}

type httpGeneration struct {
	listener  net.Listener
	server    *http.Server
	done      chan error
	serveDone chan struct{}

	handlersMu sync.Mutex
	stopped    bool
	handlers   sync.WaitGroup

	// Protected by HTTP.mu.
	drained     bool
	shutdownErr error
}

// NewHTTP reuses handler across starts; it does not construct handlers or start
// application jobs. A nil handler uses http.DefaultServeMux, as http.Server does.
// A nil logger uses slog.Default().
func NewHTTP(addr string, handler http.Handler, logger *slog.Logger) *HTTP {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTP{addr: addr, handler: handler, logger: logger}
}

// Start binds synchronously, so bind failures are returned to the caller. It is
// a no-op while running. The first successful bind fixes the address (including
// an automatically chosen port) for all subsequent starts. After a Serve failure,
// Start first drains the previous generation before starting another one.
func (h *HTTP) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if g := h.current; g != nil {
		if h.URL() != "" {
			return nil
		}
		if err := h.shutdown(g, context.Background()); err != nil {
			// A previous, fully drained shutdown may have timed out. Its
			// result must not prevent a new generation from starting.
			h.logger.Debug("http_previous_shutdown", "error", err)
		}
	}

	listener, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("listen HTTP on %s: %w", h.addr, err)
	}
	h.addr = listener.Addr().String()
	g := &httpGeneration{
		listener:  listener,
		done:      make(chan error, 1),
		serveDone: make(chan struct{}),
	}
	g.server = rest.HTTPServer(h.addr, g.track(h.handler))
	g.server.ErrorLog = slog.NewLogLogger(h.logger.Handler(), slog.LevelError)
	h.stateMu.Lock()
	h.current = g
	h.url = "http://" + h.addr
	h.stateMu.Unlock()

	go h.serve(g)
	h.logger.Info("http_started", "url", "http://"+h.addr)
	return nil
}

// URL returns the actual bound HTTP URL, or an empty string before Start, after
// Serve exits (including while Shutdown drains), or after a failed initial bind.
func (h *HTTP) URL() string {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	return h.url
}

// Done returns the current generation's buffered error signal. Capture it after
// Start; a restart creates a different channel. It is nil until the first
// successful Start, and a failed bind does not replace it. Serve sends exactly
// one error on unexpected failure, and no value on normal shutdown, then closes
// the channel. A closed channel is not itself an error. Done signals Serve exit,
// NOT handler completion; use Shutdown before releasing handler dependencies.
func (h *HTTP) Done() <-chan error {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	if h.current == nil {
		return nil
	}
	return h.current.done
}

// Shutdown gracefully stops serving and falls back to Close if Shutdown fails.
// It always waits for all admitted handler executions to return, even after ctx
// expires: closing connections alone does not stop handlers using a database.
// Consequently a handler ignoring cancellation can delay this call indefinitely.
// Handlers must not call Start or Shutdown synchronously, nor leave dependency
// work running after ServeHTTP returns. Repeated calls for a generation return
// its original shutdown result. Start and Shutdown are fully serialized.
func (h *HTTP) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current == nil {
		return nil
	}
	return h.shutdown(h.current, ctx)
}

// shutdown is called with h.mu held.
func (h *HTTP) shutdown(g *httpGeneration, ctx context.Context) error {
	if g.drained {
		return g.shutdownErr
	}
	err := g.server.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, g.server.Close())
	}
	// Serve may not have started yet, or a connection goroutine may not have
	// entered the wrapper yet. Wait for the admission gate to close before Wait.
	<-g.serveDone
	g.handlers.Wait()
	g.drained = true
	g.shutdownErr = err
	h.logger.Info("http_stopped", "address", g.listener.Addr().String())
	return err
}

func (h *HTTP) serve(g *httpGeneration) {
	err := g.server.Serve(g.listener)
	g.handlersMu.Lock()
	g.stopped = true
	g.handlersMu.Unlock()

	h.stateMu.Lock()
	h.url = ""
	h.stateMu.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		err = fmt.Errorf("serve HTTP on %s: %w", g.listener.Addr(), err)
		h.logger.Error("http_serve_failed", "error", err)
		g.done <- err
	}
	close(g.done)
	close(g.serveDone)
}

func (g *httpGeneration) track(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.handlersMu.Lock()
		if g.stopped {
			g.handlersMu.Unlock()
			http.Error(w, "server stopped", http.StatusServiceUnavailable)
			return
		}
		g.handlers.Add(1)
		g.handlersMu.Unlock()
		defer g.handlers.Done()
		handler.ServeHTTP(w, r)
	})
}
