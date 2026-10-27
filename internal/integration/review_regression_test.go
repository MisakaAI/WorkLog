package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"worklog/internal/domain"
	"worklog/internal/repository"
	"worklog/internal/transport/rest"
)

func TestRESTBearerCannotEscalateDestructive(t *testing.T) {
	f := setup(t, "sqlite")
	task := httpOK(t, f, "POST", "/api/tasks", map[string]any{"title": "keep", "idempotencyKey": "keep"})
	bearer := map[string]string{"Authorization": "Bearer " + f.cfg.WriteToken}
	code, _, _ := req(t, f, "DELETE", "/api/tasks/"+task["id"].(string), map[string]any{"version": 1}, bearer)
	if code != 403 {
		t.Fatal(code)
	}
	d := typed[domain.Dashboard](t, httpOK(t, f, "GET", "/api/dashboard", nil))
	d.Tasks = []domain.Task{}
	code, _, _ = req(t, f, "PUT", "/api/dashboard", d, bearer)
	if code != 403 {
		t.Fatal("snapshot deletion", code)
	}
	code, _, _ = req(t, f, "PUT", "/api/worklog/2026-10-21", map[string]any{"version": 0, "content": domain.EmptyWorklog("").Content}, bearer)
	if code != 403 {
		t.Fatal("empty log", code)
	}
	todo := httpOK(t, f, "POST", "/api/todos", map[string]any{"title": "keep todo", "idempotencyKey": "keep-todo"})
	code, _, _ = req(t, f, "DELETE", "/api/todos/"+todo["id"].(string), map[string]any{"version": 1}, bearer)
	if code != 403 {
		t.Fatal("todo deletion", code)
	}
	httpOK(t, f, "DELETE", "/api/tasks/"+task["id"].(string), map[string]any{"version": 1})
}

func TestInvalidIdempotencyKeys(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("WORKLOG_TEST_POSTGRES_DSN") == "" {
				t.Skip("WORKLOG_TEST_POSTGRES_DSN is not set")
			}
			f := setup(t, driver)
			for _, key := range []string{"nul\x00key", "line\nkey", "tab\tkey", strings.Repeat("中", 67)} {
				_, err := f.svc.Execute(ctx, owner, "create_task", raw(map[string]any{"title": "task", "idempotencyKey": key}))
				status(t, err, 400)
			}
		})
	}
}

func TestLegacyNanosecondTimestampRead(t *testing.T) {
	f := setup(t, "sqlite")
	timestamp := "2026-10-20T16:30:00.123456789Z"
	must(t, f.store.Within(ctx, "owner", true, func(u repository.Unit) error {
		return u.SaveWorklog(domain.Worklog{Date: "2026-10-21", Content: domain.EmptyWorklog("").Content, UpdatedAt: &timestamp, Version: 1}, false)
	}))
	entry := httpOK(t, f, "GET", "/api/worklog/2026-10-21", nil)
	if entry["updatedAt"] != "2026-10-20T16:30:00.123Z" {
		t.Fatal(entry)
	}
}

func TestHTTPBudgetsAndStaticBoundary(t *testing.T) {
	f := setup(t, "sqlite")
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "assets"), 0700))
	must(t, os.MkdirAll(filepath.Join(dir, "data"), 0700))
	calendarJSON := `{"workingWeekdays":[1,2,3,4,5],"holidays":[],"adjustedWorkdays":[]}`
	for name, contents := range map[string]string{"index.html": "<html>worklog</html>", "assets/app-hash.js": "console.log('app')", "data/worklog.db": "private database", "README.md": "private source", "work-calendar.json": calendarJSON, "config.json": `{"write_token":"private-disk-token","db_dsn":"private-disk-dsn"}`, "config.js": "window.WORKLOG_CONFIG = {secret: 'private-legacy-script'}"} {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600))
	}
	outside := filepath.Join(t.TempDir(), "private.js")
	must(t, os.WriteFile(outside, []byte("private token"), 0600))
	must(t, os.Symlink(outside, filepath.Join(dir, "assets", "escape.js")))
	cfg := f.cfg
	cfg.Local = false
	cfg.BasicUser = "human"
	cfg.BasicPassword = "private-human-password"
	cfg.StaticDir = dir
	cfg.Timezone = "Pacific/Auckland"
	cfg.DSN = "private-server-dsn"
	cfg.TokenFile = "private-token-file"
	server := httptest.NewServer(rest.New(f.svc, cfg, logger))
	defer server.Close()
	f.server = server
	// Invalid identities cannot consume the authenticated budget.
	for range 305 {
		code, _, _ := req(t, f, "GET", "/api/dashboard", nil, nil)
		if code != 401 {
			t.Fatal(code)
		}
	}
	get := func(path string) (int, http.Header, string) {
		r, e := http.NewRequest("GET", server.URL+path, nil)
		must(t, e)
		r.SetBasicAuth(cfg.BasicUser, cfg.BasicPassword)
		res, e := http.DefaultClient.Do(r)
		must(t, e)
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		must(t, e)
		return res.StatusCode, res.Header, string(b)
	}
	for range 305 {
		code, _, _ := get("/assets/app-hash.js")
		if code != 200 {
			t.Fatal(code)
		}
	}
	for range 305 {
		code, _, _ := req(t, f, "GET", "/healthz", nil, nil)
		if code != 200 {
			t.Fatal(code)
		}
	}
	for _, path := range []string{"/data/worklog.db", "/README.md", "/go.mod", "/assets/escape.js", "/assets/", "/.env", "/config.js"} {
		code, _, _ := get(path)
		if code != 404 {
			t.Fatal(path, code)
		}
	}
	code, _, _ := req(t, f, "GET", "/work-calendar.json", nil, nil)
	if code != 401 {
		t.Fatal("calendar must require authentication", code)
	}
	code, h, calendarBody := get("/work-calendar.json")
	if code != 200 || calendarBody != calendarJSON || !strings.Contains(h.Get("Content-Type"), "application/json") || !strings.Contains(h.Get("Cache-Control"), "no-store") {
		t.Fatal(code, h, calendarBody)
	}
	updatedCalendar := `{"workingWeekdays":[1,2,3,4,5,6],"holidays":[],"adjustedWorkdays":[]}`
	must(t, os.WriteFile(filepath.Join(dir, "work-calendar.json"), []byte(updatedCalendar), 0600))
	code, _, calendarBody = get("/work-calendar.json")
	if code != 200 || calendarBody != updatedCalendar {
		t.Fatal("calendar replacement must not require restart", code, calendarBody)
	}
	code, h, _ = get("/assets/app-hash.js")
	if code != 200 || !strings.Contains(h.Get("Cache-Control"), "private, max-age=") {
		t.Fatal(code, h)
	}
	code, h, body := get("/config.json")
	if code != 200 || !strings.Contains(h.Get("Content-Type"), "application/json") || !strings.Contains(h.Get("Cache-Control"), "no-store") {
		t.Fatal("public configuration response", code, h)
	}
	var publicConfig map[string]any
	must(t, json.Unmarshal([]byte(body), &publicConfig))
	// An exact field allowlist prevents credentials, database paths and other
	// server settings (or the private on-disk config.json) from reaching browsers.
	if len(publicConfig) != 2 || publicConfig["apiBaseUrl"] != "/api" || publicConfig["businessTimezone"] != cfg.Timezone {
		t.Fatal("public configuration must contain only the configured browser settings")
	}
	for _, secret := range []string{cfg.WriteToken, cfg.ReadToken, cfg.BasicPassword, cfg.DSN, cfg.TokenFile, "private-disk-token", "private-disk-dsn", "private-legacy-script"} {
		if strings.Contains(body, secret) {
			t.Fatal("public configuration leaked private settings")
		}
	}
	nav, e := http.NewRequest("GET", server.URL+"/", nil)
	must(t, e)
	nav.SetBasicAuth(cfg.BasicUser, cfg.BasicPassword)
	nav.Header.Set("Sec-Fetch-Site", "cross-site")
	nav.Header.Set("Sec-Fetch-Mode", "navigate")
	nav.Header.Set("Sec-Fetch-Dest", "document")
	res, e := http.DefaultClient.Do(nav)
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	must(t, os.Remove(filepath.Join(dir, "index.html")))
	code, _, body = get("/")
	if code != 404 || strings.Contains(body, "README.md") {
		t.Fatal("directory listing", code, body)
	}
	must(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>worklog</html>"), 0600))
	for range 300 {
		code, _, _ := get("/api/dashboard")
		if code != 200 {
			t.Fatal(code)
		}
	}
	code, h, _ = get("/api/dashboard")
	if code != 429 || h.Get("Retry-After") == "" {
		t.Fatal(code, h)
	}
	code, _, _ = get("/")
	if code != 200 {
		t.Fatal("static starved", code)
	}
	code, _, _ = req(t, f, "GET", "/healthz", nil, nil)
	if code != 200 {
		t.Fatal("probe starved", code)
	}
}

type gatedRepository struct {
	repository.Repository
	entered chan struct{}
	release chan struct{}
}

func (g *gatedRepository) Within(ctx context.Context, user string, write bool, fn func(repository.Unit) error) error {
	select {
	case g.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.Repository.Within(ctx, user, write, fn)
}

func TestMCP32ConcurrentCallsDoNotAcquireTwice(t *testing.T) {
	f := setup(t, "sqlite")
	gate := &gatedRepository{Repository: f.store, entered: make(chan struct{}, 32), release: make(chan struct{})}
	f.svc.Repo = gate
	client := mcp.NewClient(&mcp.Implementation{Name: "concurrency-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport, f.cfg.WriteToken}}}, nil)
	must(t, e)
	defer session.Close()
	results := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
			if e == nil && result.IsError {
				b, _ := json.Marshal(result)
				e = &toolFailure{string(b)}
			}
			results <- e
		})
	}
	// All 32 must reach the business boundary simultaneously. A nested acquire
	// would reject some before they can enter, making this time out.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	reached := 0
	for reached < 32 {
		select {
		case <-gate.entered:
			reached++
		case <-timer.C:
			close(gate.release)
			wg.Wait()
			t.Fatalf("only %d calls reached service", reached)
		}
	}
	close(gate.release)
	wg.Wait()
	close(results)
	for e := range results {
		must(t, e)
	}
}

type toolFailure struct{ message string }

func (e *toolFailure) Error() string { return e.message }
