package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"worklog/internal/config"
	"worklog/internal/domain"
	"worklog/internal/repository"
	"worklog/internal/service"
	"worklog/internal/storage"
	mcptransport "worklog/internal/transport/mcp"
	"worklog/internal/transport/rest"
	"worklog/internal/transport/security"
)

var ctx = context.Background()
var owner = domain.Identity{User: "owner", Write: true, Destructive: true}
var logger = slog.New(slog.NewJSONHandler(io.Discard, nil))

type fixture struct {
	store       *storage.Store
	svc         *service.Service
	server      *httptest.Server
	dsn, driver string
	cfg         config.Config
}

func setup(t *testing.T, driver string) *fixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "worklog.db")
	if driver == "postgres" {
		base := os.Getenv("WORKLOG_TEST_POSTGRES_DSN")
		if base == "" {
			t.Fatal("PostgreSQL test configuration missing")
		}
		db, e := sql.Open("pgx", base)
		must(t, e)
		schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, e = db.Exec("CREATE SCHEMA " + schema)
		must(t, e)
		t.Cleanup(func() {
			_, e = db.Exec("DROP SCHEMA " + schema + " CASCADE")
			if e != nil {
				t.Error(e)
			}
			_ = db.Close()
		})
		u, e := url.Parse(base)
		must(t, e)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	st, e := storage.Open(ctx, driver, dsn)
	must(t, e)
	t.Cleanup(func() { _ = st.Close() })
	loc, e := time.LoadLocation("Asia/Shanghai")
	must(t, e)
	svc := service.New(st, loc)
	svc.Now = func() time.Time { return time.Date(2026, 10, 20, 16, 30, 0, 123456789, time.UTC) }
	cfg := config.Config{Local: true, User: "owner", AllowedHosts: []string{"127.0.0.1", "localhost"}, AllowedOrigins: []string{"http://localhost:5173"}, WriteToken: strings.Repeat("w", 32), ReadToken: strings.Repeat("r", 32), MCPDestructive: false}
	ts := httptest.NewServer(rest.New(svc, cfg, logger))
	t.Cleanup(ts.Close)
	return &fixture{st, svc, ts, dsn, driver, cfg}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func status(t *testing.T, e error, want int) {
	t.Helper()
	if e == nil || domain.Public(e).Status != want {
		t.Fatalf("want error %d, got %v", want, e)
	}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func invoke(t *testing.T, f *fixture, id domain.Identity, op string, in any) any {
	t.Helper()
	v, e := f.svc.Execute(ctx, id, op, raw(in))
	must(t, e)
	return v
}
func typed[T any](t *testing.T, v any) T {
	t.Helper()
	var out T
	must(t, json.Unmarshal(raw(v), &out))
	return out
}
func req(t *testing.T, f *fixture, method, path string, in any, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var b io.Reader
	if in != nil {
		b = bytes.NewReader(raw(in))
	}
	r, e := http.NewRequest(method, f.server.URL+path, b)
	must(t, e)
	if in != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, e := http.DefaultClient.Do(r)
	must(t, e)
	defer res.Body.Close()
	var out map[string]any
	err := json.NewDecoder(res.Body).Decode(&out)
	if err != io.EOF || method != "OPTIONS" {
		must(t, err)
	}
	return res.StatusCode, out, res.Header
}
func httpOK(t *testing.T, f *fixture, method, path string, in any) map[string]any {
	t.Helper()
	code, out, _ := req(t, f, method, path, in, nil)
	if code != 200 {
		t.Fatalf("%s %s: %d %v", method, path, code, out)
	}
	return out
}
func snapshotFixture() domain.Dashboard {
	return domain.Dashboard{Revision: 0, PersonalLabel: "个人工作台", Projects: []domain.Project{{ID: "p", Name: "Project"}}, Categories: []string{"A", "B"}, Tasks: []domain.Task{{ID: domain.StringID("t1"), Title: "one", ProjectID: "p", Status: "planned", Tag: "A", Priority: "中", Color: "white"}, {ID: domain.StringID("t2"), Title: "two", ProjectID: "p", Status: "progress", Tag: "B", Priority: "高", Color: "red", StartDate: "2026-10-20", Date: "2026-10-22"}}, Todos: []domain.Todo{{ID: domain.StringID("todo"), Title: "todo"}}}
}
func suite(t *testing.T, driver string) {
	t.Run("RESTEndpointsAndPersistence", func(t *testing.T) {
		f := setup(t, driver)
		initial := httpOK(t, f, "GET", "/api/dashboard", nil)
		if initial["revision"] != float64(0) || len(initial["tasks"].([]any)) != 0 {
			t.Fatal(initial)
		}
		project := httpOK(t, f, "PUT", "/api/projects", map[string]any{"projectRevision": 0, "projects": []domain.Project{{ID: "p", Name: " Project "}}})
		if project["projectRevision"] != float64(1) {
			t.Fatal(project)
		}
		task := httpOK(t, f, "POST", "/api/tasks", map[string]any{"title": " Task ", "projectId": "p", "idempotencyKey": "task1"})
		id := task["id"].(string)
		if task["version"] != float64(1) || task["title"] != "Task" {
			t.Fatal(task)
		}
		replay := httpOK(t, f, "POST", "/api/tasks", map[string]any{"title": " Task ", "projectId": "p", "idempotencyKey": "task1"})
		if !reflect.DeepEqual(task, replay) {
			t.Fatal("idempotent mismatch")
		}
		task = httpOK(t, f, "PATCH", "/api/tasks/"+id, map[string]any{"version": 1, "status": "done", "startDate": "2026-10-20", "date": "2026-10-21"})
		if task["version"] != float64(2) {
			t.Fatal(task)
		}
		httpOK(t, f, "GET", "/api/tasks/"+id, nil)
		listed := httpOK(t, f, "GET", "/api/tasks?status=done&startDate=2026-10-21&endDate=2026-10-22", nil)
		if len(listed["tasks"].([]any)) != 1 {
			t.Fatal(listed)
		}
		httpOK(t, f, "GET", "/api/projects", nil)
		httpOK(t, f, "GET", "/api/categories", nil)
		httpOK(t, f, "PUT", "/api/categories", map[string]any{"categoryRevision": 0, "categories": []service.CategoryDraft{{OriginalName: "开发", Name: "Engineering"}}})
		todo := httpOK(t, f, "POST", "/api/todos", map[string]any{"title": "todo", "idempotencyKey": "todo1"})
		todoid := todo["id"].(string)
		todo = httpOK(t, f, "PATCH", "/api/todos/"+todoid, map[string]any{"version": 1, "done": true})
		if todo["done"] != true {
			t.Fatal(todo)
		}
		httpOK(t, f, "GET", "/api/todos?done=true", nil)
		httpOK(t, f, "GET", "/api/today", nil)
		httpOK(t, f, "GET", "/api/worklog/dates", nil)
		httpOK(t, f, "DELETE", "/api/todos/"+todoid, map[string]any{"version": 2})
		httpOK(t, f, "DELETE", "/api/tasks/"+id, map[string]any{"version": 3}) // category migration incremented task version
		before := httpOK(t, f, "GET", "/api/dashboard", nil)
		must(t, f.store.Close())
		again, e := storage.Open(ctx, driver, f.dsn)
		must(t, e)
		defer again.Close()
		svc := service.New(again, f.svc.Location)
		after, e := svc.Dashboard(ctx, owner)
		must(t, e)
		if !reflect.DeepEqual(before, typed[map[string]any](t, after)) {
			t.Fatal("restart lost state")
		}
	})
	t.Run("AtomicSnapshotAndConcurrentRevisions", func(t *testing.T) {
		f := setup(t, driver)
		d := snapshotFixture()
		saved, e := f.svc.SaveDashboard(ctx, owner, d)
		must(t, e)
		bad := saved
		bad.Projects = []domain.Project{}
		_, e = f.svc.SaveDashboard(ctx, owner, bad)
		status(t, e, 400)
		current, e := f.svc.Dashboard(ctx, owner)
		must(t, e)
		if !reflect.DeepEqual(current, saved) {
			t.Fatal("partial save")
		}
		second, e := storage.Open(ctx, driver, f.dsn)
		must(t, e)
		defer second.Close()
		svc2 := service.New(second, f.svc.Location)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, svc := range []*service.Service{f.svc, svc2} {
			go func(s *service.Service) { <-start; _, e := s.SaveDashboard(ctx, owner, saved); results <- e }(svc)
		}
		close(start)
		ok, conflicts := 0, 0
		for range 2 {
			e := <-results
			if e == nil {
				ok++
			} else if domain.Public(e).Status == 409 {
				conflicts++
			} else {
				t.Fatal(e)
			}
		}
		if ok != 1 || conflicts != 1 {
			t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
		}
		_, e = f.svc.SaveDashboard(ctx, owner, saved)
		status(t, e, 409)
	})
	t.Run("ProjectMigrationCategorySwapsAndRollback", func(t *testing.T) {
		f := setup(t, driver)
		_, e := f.svc.SaveDashboard(ctx, owner, snapshotFixture())
		must(t, e)
		result := typed[service.CollectionResult](t, invoke(t, f, owner, "delete_project", service.DeleteProject{ID: "p", ExpectedRevision: 1}))
		if len(result.UpdatedTasks) != 2 || len(result.Projects) != 0 {
			t.Fatal(result)
		}
		for _, t1 := range result.UpdatedTasks {
			if t1.ProjectID != "" || t1.Version != 2 {
				t.Fatal(t1)
			}
		}
		result = typed[service.CollectionResult](t, invoke(t, f, owner, "manage_categories", service.CategoryChange{ExpectedRevision: 1, Categories: []service.CategoryDraft{{OriginalName: "A", Name: "B"}, {OriginalName: "B", Name: "A"}}}))
		if len(result.UpdatedTasks) != 2 || result.UpdatedTasks[0].Tag != "B" || result.UpdatedTasks[1].Tag != "A" {
			t.Fatal(result)
		}
		before, e := f.svc.Dashboard(ctx, owner)
		must(t, e)
		_, e = f.svc.Execute(ctx, owner, "manage_categories", raw(service.CategoryChange{ExpectedRevision: 2, Categories: []service.CategoryDraft{{OriginalName: "A", Name: "same"}, {OriginalName: "B", Name: "SAME"}}}))
		status(t, e, 400)
		after, e := f.svc.Dashboard(ctx, owner)
		must(t, e)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rollback failed")
		}
		result = typed[service.CollectionResult](t, invoke(t, f, owner, "manage_categories", service.CategoryChange{ExpectedRevision: 2, Categories: []service.CategoryDraft{{OriginalName: "A", Name: "Only"}}}))
		if result.UpdatedTasks[0].Tag != "Only" || result.UpdatedTasks[1].Tag != "Only" {
			t.Fatal(result)
		}
		// Explicit repository failure after SQL mutations must roll back the whole graph.
		e = f.store.Within(ctx, owner.User, true, func(u repository.Unit) error {
			st, e := u.Load()
			if e != nil {
				return e
			}
			st.PersonalLabel = "must rollback"
			if e = u.Save(st); e != nil {
				return e
			}
			return fmt.Errorf("injected failure")
		})
		if e == nil {
			t.Fatal("expected rollback")
		}
		after, e = f.svc.Dashboard(ctx, owner)
		must(t, e)
		if after.PersonalLabel == "must rollback" {
			t.Fatal("transaction committed on error")
		}
	})
	t.Run("VersionIdempotencyAndABA", func(t *testing.T) {
		f := setup(t, driver)
		in := service.CreateTask{Title: "task", IdempotencyKey: "stable"}
		task := typed[domain.TaskResource](t, invoke(t, f, owner, "create_task", in))
		again := typed[domain.TaskResource](t, invoke(t, f, owner, "create_task", in))
		if !reflect.DeepEqual(task, again) {
			t.Fatal("idempotent retry")
		}
		in.Title = "different"
		_, e := f.svc.Execute(ctx, owner, "create_task", raw(in))
		status(t, e, 409)
		_, e = f.svc.Execute(ctx, owner, "create_task", raw(map[string]any{"title": "x"}))
		status(t, e, 400)
		title := "changed"
		updated := typed[domain.TaskResource](t, invoke(t, f, owner, "update_task", service.UpdateTask{ID: task.ID.Key(), ExpectedVersion: 1, Title: &title}))
		if updated.Version != 2 {
			t.Fatal(updated)
		}
		_, e = f.svc.Execute(ctx, owner, "update_task", raw(service.UpdateTask{ID: task.ID.Key(), ExpectedVersion: 1, Title: &title}))
		status(t, e, 409)
		updated = typed[domain.TaskResource](t, invoke(t, f, owner, "update_task", service.UpdateTask{ID: task.ID.Key(), ExpectedVersion: 2, Title: &title}))
		if updated.Version != 3 {
			t.Fatal("no-op version")
		}
		invoke(t, f, owner, "delete_task", service.DeleteResource{ID: task.ID.Key(), ExpectedVersion: 3})
		d, e := f.svc.Dashboard(ctx, owner)
		must(t, e)
		d.Tasks = append(d.Tasks, task.Task)
		_, e = f.svc.SaveDashboard(ctx, owner, d)
		must(t, e)
		got := typed[domain.TaskResource](t, invoke(t, f, owner, "get_task", service.IDInput{ID: task.ID.Key()}))
		if got.Version != 4 {
			t.Fatal("ABA reset", got.Version)
		}
		// Identical keys are scoped to identity, not global.
		other := domain.Identity{User: "other", Write: true, Destructive: true}
		in.Title = "other"
		otherTask := typed[domain.TaskResource](t, invoke(t, f, other, "create_task", in))
		if otherTask.ID == task.ID {
			t.Fatal("shared idempotent response")
		}
	})
	t.Run("WorklogDatesTimezoneAndRoundTrip", func(t *testing.T) {
		f := setup(t, driver)
		today := httpOK(t, f, "GET", "/api/today", nil)
		if today["date"] != "2026-10-21" || today["updatedAt"] != nil || today["version"] != float64(0) {
			t.Fatal(today)
		}
		content := json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"标题","marks":[{"type":"textStyle","attrs":{"color":"#c62828"}},{"type":"bold"}]}]},{"type":"orderedList","attrs":{"start":7,"type":null},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"项目"}]},{"type":"blockquote","content":[{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"var x=1"}]}]}]}]}]}`)
		saved := httpOK(t, f, "PUT", "/api/worklog/2026-10-21", map[string]any{"content": content, "version": 0})
		if saved["version"] != float64(1) || saved["updatedAt"] != "2026-10-20T16:30:00.123Z" {
			t.Fatal(saved)
		}
		read := httpOK(t, f, "GET", "/api/worklog/2026-10-21", nil)
		if !reflect.DeepEqual(typed[any](t, content), read["content"]) {
			t.Fatal("format lost")
		}
		dates := httpOK(t, f, "GET", "/api/worklog/dates", nil)
		if len(dates["dates"].([]any)) != 1 {
			t.Fatal(dates)
		}
		code, _, _ := req(t, f, "PUT", "/api/worklog/2026-10-21", map[string]any{"content": content, "version": 0}, nil)
		if code != 409 {
			t.Fatal(code)
		}
		httpOK(t, f, "PUT", "/api/worklog/2026-10-21", map[string]any{"content": domain.EmptyWorklog("").Content, "version": 1})
		dates = httpOK(t, f, "GET", "/api/worklog/dates", nil)
		if len(dates["dates"].([]any)) != 0 {
			t.Fatal(dates)
		}
		httpOK(t, f, "PUT", "/api/worklog/2099-01-01", map[string]any{"content": json.RawMessage(`{"type":"doc","content":[{"type":"horizontalRule"}]}`), "version": 0})
		httpOK(t, f, "GET", "/api/worklog/1900-01-01", nil)
		_, e := f.svc.SaveWorklog(ctx, owner, "2026-10-21", []byte(`{"type":"doc","content":[{"type":"image"}]}`), 2)
		status(t, e, 400)
		w, e := f.svc.Worklog(ctx, owner, "2026-10-21")
		must(t, e)
		if w.Version != 2 {
			t.Fatal("invalid document mutated")
		}
		for _, date := range []string{"2026-02-30", "2026-2-01", "not-date"} {
			code, out, _ := req(t, f, "GET", "/api/worklog/"+date, nil, nil)
			if code != 400 || out["requestId"] == nil {
				t.Fatal(code, out)
			}
		}
	})
	t.Run("OwnershipPermissionsAndErrors", func(t *testing.T) {
		f := setup(t, driver)
		task := typed[domain.TaskResource](t, invoke(t, f, owner, "create_task", service.CreateTask{Title: "secret", IdempotencyKey: "k"}))
		other := domain.Identity{User: "other", Write: true, Destructive: true}
		_, e := f.svc.Execute(ctx, other, "get_task", raw(service.IDInput{ID: task.ID.Key()}))
		status(t, e, 404)
		_, e = f.svc.Execute(ctx, other, "update_task", raw(service.UpdateTask{ID: task.ID.Key(), ExpectedVersion: 1}))
		status(t, e, 404)
		otherDashboard := snapshotFixture()
		_, e = f.svc.SaveDashboard(ctx, other, otherDashboard)
		must(t, e)
		in := service.CreateTask{Title: "bad ref", ProjectID: "p", IdempotencyKey: "badref"}
		_, e = f.svc.Execute(ctx, owner, "create_task", raw(in))
		status(t, e, 400)
		_, e = f.svc.Dashboard(ctx, domain.Identity{})
		status(t, e, 401)
		_, e = f.svc.Execute(ctx, domain.Identity{User: "owner"}, "create_task", raw(in))
		status(t, e, 403)
		_, e = f.svc.Execute(ctx, domain.Identity{User: "owner", Write: true}, "delete_task", raw(service.DeleteResource{ID: task.ID.Key(), ExpectedVersion: 1}))
		status(t, e, 403)
		for _, tc := range []struct {
			path    string
			headers map[string]string
			code    int
		}{{"/api/dashboard?userId=other", nil, 400}, {"/missing", nil, 404}, {"/api/dashboard", map[string]string{"Origin": "https://evil.example"}, 403}, {"/api/dashboard", map[string]string{"Authorization": "Bearer invalid"}, 401}} {
			code, out, h := req(t, f, "GET", tc.path, nil, tc.headers)
			if code != tc.code || out["error"] == nil || out["requestId"] == nil || h.Get("Cache-Control") != "private, no-store" {
				t.Fatal(tc, code, out, h)
			}
		}
		code, out, _ := req(t, f, "POST", "/api/tasks", map[string]any{"title": "x", "idempotencyKey": "x", "userId": "other"}, nil)
		if code != 400 || out["error"] == nil {
			t.Fatal(code, out)
		}
		code, _, _ = req(t, f, "PUT", "/api/dashboard", map[string]any{"huge": strings.Repeat("x", 10<<20)}, nil)
		if code != 413 {
			t.Fatal("oversize", code)
		}
		code, _, _ = req(t, f, "PUT", "/api/worklog/2026-10-21", map[string]any{"content": strings.Repeat("x", 1<<20), "version": 0}, nil)
		if code != 413 {
			t.Fatal("log oversize", code)
		}
		code, _, h := req(t, f, "OPTIONS", "/api/dashboard", nil, map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "PUT"})
		if code != 200 || h.Get("Access-Control-Allow-Origin") != "http://localhost:5173" || h.Get("Access-Control-Max-Age") != "600" {
			t.Fatal(code, h)
		}
	})
	t.Run("ActualMCPAndRESTConsistency", func(t *testing.T) {
		f := setup(t, driver)
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport, f.cfg.WriteToken}}}, nil)
		must(t, e)
		defer session.Close()
		tools, e := session.ListTools(ctx, nil)
		must(t, e)
		if len(tools.Tools) != 21 {
			t.Fatalf("tools=%d", len(tools.Tools))
		}
		for _, tool := range tools.Tools {
			if tool.Name == "save_worklog" && (tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
				t.Fatal("save_worklog must advertise possible destructive behavior")
			}
		}
		call := func(name string, args any, wantError bool) *mcp.CallToolResult {
			t.Helper()
			res, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			must(t, e)
			if res.IsError != wantError {
				t.Fatalf("%s error=%v result=%v", name, res.IsError, res.Content)
			}
			return res
		}
		res := call("create_task", map[string]any{"title": "MCP task", "idempotencyKey": "mcp-task"}, false)
		var task domain.TaskResource
		must(t, json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &task))
		listed := httpOK(t, f, "GET", "/api/tasks", nil)
		if listed["tasks"].([]any)[0].(map[string]any)["id"] != task.ID.Key() {
			t.Fatal("different data")
		}
		call("update_task", map[string]any{"id": task.ID.Key(), "expectedVersion": 1, "title": "MCP changed"}, false)
		call("update_task", map[string]any{"id": task.ID.Key(), "expectedVersion": 1, "title": "stale"}, true)
		call("move_task", map[string]any{"id": task.ID.Key(), "expectedVersion": 2, "status": "done", "projectId": ""}, false)
		call("delete_task", map[string]any{"id": task.ID.Key(), "expectedVersion": 3}, true)
		call("create_task", map[string]any{"title": "bad", "idempotencyKey": "mcp-bad", "userId": "other"}, true)
		call("create_task", map[string]any{"idempotencyKey": "mcp-no-title"}, true)
		call("save_worklog", map[string]any{"date": "2026-10-21", "expectedVersion": 0, "content": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "horizontalRule"}}}}, false)
		dates := httpOK(t, f, "GET", "/api/worklog/dates", nil)
		if len(dates["dates"].([]any)) != 1 {
			t.Fatal(dates)
		}
		call("save_worklog", map[string]any{"date": "2026-10-21", "expectedVersion": 1, "content": typed[any](t, domain.EmptyWorklog("").Content)}, true)
		call("get_dashboard_summary", map[string]any{}, false)
		readClient := mcp.NewClient(&mcp.Implementation{Name: "read-only", Version: "1"}, nil)
		readSession, e := readClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport, f.cfg.ReadToken}}}, nil)
		must(t, e)
		defer readSession.Close()
		res, e = readSession.CallTool(ctx, &mcp.CallToolParams{Name: "create_todo", Arguments: map[string]any{"title": "not allowed", "idempotencyKey": "read"}})
		must(t, e)
		if !res.IsError {
			t.Fatal("read token wrote")
		}
		// Independent local-process transport exercises the same official SDK tools.
		a, b := mcp.NewInMemoryTransports()
		server := mcptransport.New(f.svc, owner, logger, security.NewLimiter())
		ss, e := server.Connect(ctx, a, nil)
		must(t, e)
		defer ss.Close()
		local, e := client.Connect(ctx, b, nil)
		must(t, e)
		defer local.Close()
		localCall := func(name string, args any) *mcp.CallToolResult {
			t.Helper()
			res, e := local.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			must(t, e)
			if res.IsError {
				t.Fatalf("%s failed: %v", name, res.Content)
			}
			return res
		}
		localCall("delete_task", map[string]any{"id": task.ID.Key(), "expectedVersion": 3})
		localCall("list_projects", map[string]any{})
		projectResult := localCall("create_project", map[string]any{"name": "AI project", "expectedRevision": 0, "idempotencyKey": "project-key"})
		var projectCollection service.CollectionResult
		must(t, json.Unmarshal([]byte(projectResult.Content[0].(*mcp.TextContent).Text), &projectCollection))
		pid := projectCollection.Projects[0].ID
		localCall("rename_project", map[string]any{"id": pid, "name": "Renamed", "expectedRevision": 1})
		localCall("reorder_projects", map[string]any{"ids": []string{pid}, "expectedRevision": 2})
		localCall("delete_project", map[string]any{"id": pid, "expectedRevision": 3})
		localCall("list_categories", map[string]any{})
		localCall("manage_categories", map[string]any{"expectedRevision": 0, "categories": []map[string]any{{"originalName": "开发", "name": "AI"}}})
		todoResult := localCall("create_todo", map[string]any{"title": "AI todo", "idempotencyKey": "ai-todo"})
		var todo domain.TodoResource
		must(t, json.Unmarshal([]byte(todoResult.Content[0].(*mcp.TextContent).Text), &todo))
		localCall("list_todos", map[string]any{"done": false})
		localCall("set_todo_done", map[string]any{"id": todo.ID.Key(), "expectedVersion": 1, "done": true})
		localCall("delete_todo", map[string]any{"id": todo.ID.Key(), "expectedVersion": 2})
		localCall("list_tasks", map[string]any{})
		localCall("get_worklog", map[string]any{"date": "2026-10-21"})
		localCall("get_worklog_dates", map[string]any{})
	})
	t.Run("ConcurrentIdempotencyAndLogVersions", func(t *testing.T) {
		f := setup(t, driver)
		var wg sync.WaitGroup
		results := make(chan any, 12)
		errs := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, e := f.svc.Execute(ctx, owner, "create_todo", raw(service.CreateTodo{Title: "one", IdempotencyKey: "concurrent"}))
				results <- out
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			must(t, e)
		}
		id := ""
		for v := range results {
			todo := typed[domain.TodoResource](t, v)
			if id != "" && id != todo.ID.Key() {
				t.Fatal("duplicate create")
			}
			id = todo.ID.Key()
		}
		ch := make(chan error, 2)
		for range 2 {
			go func() {
				_, e := f.svc.SaveWorklog(ctx, owner, "2026-10-21", []byte(`{"type":"doc","content":[{"type":"horizontalRule"}]}`), 0)
				ch <- e
			}()
		}
		ok := 0
		for range 2 {
			e := <-ch
			if e == nil {
				ok++
			} else {
				status(t, e, 409)
			}
		}
		if ok != 1 {
			t.Fatal("log race", ok)
		}
	})
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}
func TestSQLite(t *testing.T) { suite(t, "sqlite") }
func TestPostgres(t *testing.T) {
	if os.Getenv("WORKLOG_TEST_POSTGRES_DSN") == "" {
		t.Skip("set WORKLOG_TEST_POSTGRES_DSN to run real PostgreSQL integration suite")
	}
	suite(t, "postgres")
}

func TestSQLitePerConnectionPragmas(t *testing.T) {
	f := setup(t, "sqlite")
	for range 8 {
		conn, e := f.store.DB.Conn(ctx)
		must(t, e)
		var fk, busy int
		var mode string
		must(t, conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
		must(t, conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy))
		must(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
		if fk != 1 || busy != 5000 || mode != "wal" {
			t.Fatal(fk, busy, mode)
		}
		defer conn.Close()
	}
}
func TestSQLiteConsistentBackupRecoveryAndReaders(t *testing.T) {
	f := setup(t, "sqlite")
	task := invoke(t, f, owner, "create_task", service.CreateTask{Title: "backup", IdempotencyKey: "b"})
	_, e := f.svc.SaveWorklog(ctx, owner, "2026-10-21", []byte(`{"type":"doc","content":[{"type":"horizontalRule"}]}`), 0)
	must(t, e)
	conn, e := f.store.DB.Conn(ctx)
	must(t, e)
	defer conn.Close()
	_, e = conn.ExecContext(ctx, "BEGIN")
	must(t, e)
	var rev int
	must(t, conn.QueryRowContext(ctx, "SELECT revision FROM workspaces WHERE user_id='owner'").Scan(&rev))
	invoke(t, f, owner, "create_todo", service.CreateTodo{Title: "writer while reader", IdempotencyKey: "w"})
	var rev2 int
	must(t, conn.QueryRowContext(ctx, "SELECT revision FROM workspaces WHERE user_id='owner'").Scan(&rev2))
	if rev2 != rev {
		t.Fatal("reader snapshot changed")
	}
	_, e = conn.ExecContext(ctx, "ROLLBACK")
	must(t, e)
	backup := filepath.Join(t.TempDir(), "backup.db")
	must(t, f.store.Backup(ctx, backup))
	must(t, storage.VerifySQLite(ctx, backup))
	restored, e := storage.Open(ctx, "sqlite", backup)
	must(t, e)
	defer restored.Close()
	svc := service.New(restored, f.svc.Location)
	out, e := svc.Execute(ctx, owner, "get_task", raw(service.IDInput{ID: typed[domain.TaskResource](t, task).ID.Key()}))
	must(t, e)
	if !reflect.DeepEqual(raw(task), raw(out)) {
		t.Fatal("restored task differs")
	}
	dates, e := svc.Dates(ctx, owner, "", "")
	must(t, e)
	if len(dates) != 1 {
		t.Fatal("restored worklogs missing")
	}
	if e = f.store.Backup(ctx, backup); e == nil {
		t.Fatal("backup overwrite allowed")
	}
}
