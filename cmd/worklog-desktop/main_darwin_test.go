//go:build darwin && desktop && cgo

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"worklog/internal/app"
	"worklog/internal/desktop"
	"worklog/internal/storage"
)

type testStatus chan string

func (s testStatus) SetStatus(status string) { s <- status }

func (s testStatus) wait(t *testing.T, want string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case got := <-s:
			if got == want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

type testHost struct {
	cancel        context.CancelFunc
	open, restart chan struct{}
	browser       chan *app.HTTP
	status        testStatus
	done          chan struct{}
	backups       chan desktop.BackupRequest
	notices       chan testNotice
}

type testNotice struct {
	failed  bool
	message string
}

func newTestHost(t *testing.T, conf string, options ...serviceOptions) *testHost {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &testHost{cancel: cancel, open: make(chan struct{}, 1), restart: make(chan struct{}, 1), browser: make(chan *app.HTTP, 1), status: make(testStatus, 32), done: make(chan struct{})}
	h.backups, h.notices = make(chan desktop.BackupRequest, 1), make(chan testNotice, 8)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	settings := serviceOptions{}
	if len(options) != 0 {
		settings = options[0]
	}
	if settings.backups == nil {
		settings.backups = h.backups
	}
	if settings.notify == nil {
		settings.notify = func(failed bool, message string) { h.notices <- testNotice{failed, message} }
	}
	go func() {
		defer close(h.done)
		runService(ctx, conf, logger, h.status, h.open, h.restart, h.browser, fstest.MapFS{"index.html": {Data: []byte("embedded-test-ui")}}, settings)
	}()
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *testHost) notice(t *testing.T, failed bool) string {
	t.Helper()
	select {
	case notice := <-h.notices:
		if notice.failed != failed {
			t.Fatalf("unexpected notification: %s", notice.message)
		}
		return notice.message
	case <-time.After(10 * time.Second):
		t.Fatal("no operation notification")
		return ""
	}
}

func macServiceRequest(t *testing.T, address, method, path, body string, auth bool) (int, string) {
	t.Helper()
	r, err := http.NewRequest(method, address+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth {
		r.SetBasicAuth("worklog", "password-123")
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
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

func TestMacMenuBackupRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	writeTestConfig(t, map[string]any{"basic_password": "password-123"})
	h := newTestHost(t, "config.json")
	h.status.wait(t, "● 服务运行中")
	originalServer := h.browserServer(t)
	address := originalServer.URL()
	if code, body := macServiceRequest(t, address, "POST", "/api/tasks", `{"title":"from-backup","idempotencyKey":"mac-backup-1"}`, true); code != 200 {
		t.Fatal(code, body)
	}
	token, err := os.ReadFile("data/mcp-token")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	h.backups <- desktop.BackupRequest{Path: directory}
	h.status.wait(t, "● 正在导出备份")
	h.notice(t, false)
	h.status.wait(t, "● 服务运行中")
	files, err := filepath.Glob(filepath.Join(directory, "worklog-backup-*.db"))
	if err != nil || len(files) != 1 {
		t.Fatal("missing backup", files, err)
	}
	if err := storage.VerifySQLite(context.Background(), files[0]); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(files[0]); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("non-private backup", info, err)
	}
	if code, body := macServiceRequest(t, address, "POST", "/api/tasks", `{"title":"after-backup","idempotencyKey":"mac-backup-2"}`, true); code != 200 {
		t.Fatal(code, body)
	}
	h.backups <- desktop.BackupRequest{Import: true, Path: files[0]}
	h.status.wait(t, "● 正在导入备份")
	h.notice(t, false)
	h.status.wait(t, "● 服务运行中")
	if originalServer.URL() != "" {
		t.Fatal("old HTTP generation remains available after import")
	}
	h.open <- struct{}{}
	server := h.browserServer(t)
	if server == originalServer {
		t.Fatal("import reused dependencies of closed database")
	}
	if code, body := macServiceRequest(t, server.URL(), "GET", "/api/tasks", "", true); code != 200 || !strings.Contains(body, "from-backup") || strings.Contains(body, "after-backup") {
		t.Fatal("incorrect restored view", code, body)
	}
	if code, _ := macServiceRequest(t, server.URL(), "GET", "/api/tasks", "", false); code != 401 {
		t.Fatal("restore bypassed authentication", code)
	}
	if after, err := os.ReadFile("data/mcp-token"); err != nil || string(after) != string(token) {
		t.Fatal("restore changed token", err)
	}
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var configuration map[string]string
	if err := json.Unmarshal(data, &configuration); err != nil {
		t.Fatal(err)
	}
	if configuration["basic_password"] != "password-123" || !strings.Contains(configuration["db_dsn"], "worklog-restored-") {
		t.Fatal("config fields lost or incorrect database selected", configuration)
	}
	original, err := storage.Open(context.Background(), "sqlite", filepath.Join(root, "data", "worklog.db"))
	if err != nil {
		t.Fatal("original database removed", err)
	}
	defer original.Close()
	var count int
	if err := original.DB.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil || count != 2 {
		t.Fatal("original data not preserved", count, err)
	}
	h.stop(t)
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	if err := desktop.SecureDataDir(root); err != nil {
		t.Fatal("restore left non-private application files", err)
	}
}

func TestMacMenuBackupFailuresKeepOriginalService(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t, map[string]any{"basic_password": "password-123"})
	h := newTestHost(t, "config.json")
	h.status.wait(t, "● 服务运行中")
	server := h.browserServer(t)
	if code, body := macServiceRequest(t, server.URL(), "POST", "/api/tasks", `{"title":"keep-on-failure","idempotencyKey":"mac-failed-import"}`, true); code != 200 {
		t.Fatal(code, body)
	}
	before, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("not-a-directory", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	h.backups <- desktop.BackupRequest{Path: "not-a-directory"}
	h.status.wait(t, "● 正在导出备份")
	h.notice(t, true)
	h.status.wait(t, "● 服务运行中")
	if server.URL() == "" {
		t.Fatal("export failure stopped HTTP")
	}
	if err := os.WriteFile("corrupt.db", []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	h.backups <- desktop.BackupRequest{Import: true, Path: "corrupt.db"}
	h.status.wait(t, "● 正在导入备份")
	h.notice(t, true)
	h.status.wait(t, "● 服务运行中")
	if after, err := os.ReadFile("config.json"); err != nil || string(after) != string(before) {
		t.Fatal("failed import switched configuration", err)
	}
	h.open <- struct{}{}
	server = h.browserServer(t)
	if code, body := macServiceRequest(t, server.URL(), "GET", "/api/tasks", "", true); code != 200 || !strings.Contains(body, "keep-on-failure") {
		t.Fatal("failed to restart original service", code, body)
	}
	writeTestConfig(t, map[string]any{"basic_password": "password-123", "timezone": "UTC"})
	address := server.URL()
	h.backups <- desktop.BackupRequest{Import: true, Path: "corrupt.db"}
	if message := h.notice(t, true); !strings.Contains(message, "配置文件已修改") {
		t.Fatal("did not reject changed configuration", message)
	}
	if server.URL() != address {
		t.Fatal("changed-config rejection interrupted active service")
	}
}

func TestMacLoginStartupStaysInMenuBar(t *testing.T) {
	t.Chdir(t.TempDir())
	h := newTestHost(t, "", serviceOptions{background: true})
	h.status.wait(t, "● 服务运行中")
	// A restart barrier ensures all startup work has finished before checking.
	h.restart <- struct{}{}
	h.status.wait(t, "● 服务重启中")
	h.status.wait(t, "● 服务运行中")
	select {
	case <-h.browser:
		t.Fatal("login startup opened browser")
	default:
	}
	h.open <- struct{}{}
	if h.browserServer(t).URL() == "" {
		t.Fatal("explicit open failed after login startup")
	}
}

func (h *testHost) stop(t *testing.T) {
	t.Helper()
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("service worker did not stop")
	}
}

func (h *testHost) browserServer(t *testing.T) *app.HTTP {
	t.Helper()
	select {
	case server := <-h.browser:
		return server
	case <-time.After(10 * time.Second):
		t.Fatal("no ready browser URL")
		return nil
	}
}

func writeTestConfig(t *testing.T, fields map[string]any) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMacServiceRestartRetainsDataAuthAndPort(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	writeTestConfig(t, map[string]any{"basic_password": "password-123"})
	h := newTestHost(t, "config.json")
	h.status.wait(t, "● 服务运行中")
	server := h.browserServer(t)
	address := server.URL()
	if !strings.HasPrefix(address, "http://127.0.0.1:") {
		t.Fatal("invalid bound URL", address)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	request := func(method, path, body string, auth bool) (int, string) {
		t.Helper()
		r, err := http.NewRequest(method, address+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if auth {
			r.SetBasicAuth("worklog", "password-123")
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
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
	if code, body := request("POST", "/api/tasks", `{"title":"mac-restart-keeps-task","idempotencyKey":"mac-restart"}`, true); code != 200 {
		t.Fatal(code, body)
	}
	token, err := os.ReadFile("data/mcp-token")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a blocked browser worker: its queue is full throughout restart.
	h.browser <- server
	h.open <- struct{}{}
	h.restart <- struct{}{}
	h.status.wait(t, "● 服务重启中")
	h.status.wait(t, "● 服务运行中")
	if server.URL() != address {
		t.Fatal("restart changed port")
	}
	if after, err := os.ReadFile("data/mcp-token"); err != nil || string(after) != string(token) {
		t.Fatal("restart changed token", err)
	}
	if code, body := request("GET", "/api/tasks", "", true); code != 200 || !strings.Contains(body, "mac-restart-keeps-task") {
		t.Fatal(code, body)
	}
	for _, path := range []string{"/", "/api/tasks", "/config.json"} {
		if code, _ := request("GET", path, "", false); code != 401 {
			t.Fatalf("auth bypass at %s: %d", path, code)
		}
	}
	if code, body := request("GET", "/", "", true); code != 200 || body != "embedded-test-ui" {
		t.Fatal("embedded UI", code, body)
	}
	if code, body := request("GET", "/config.json", "", true); code != 200 || strings.Contains(body, string(token)) || !strings.Contains(body, "/api") {
		t.Fatal("runtime config", code, body)
	}
	if code, _ := request("GET", "/data/mcp-token", "", true); code != 404 {
		t.Fatal("private path exposed", code)
	}
	h.stop(t)
	if err := desktop.SecureDataDir(root); err != nil {
		t.Fatal("service left files that prevent the next launch", err)
	}
	if server.URL() != "" {
		t.Fatal("URL remains ready after stop")
	}
	listener, err := net.Listen("tcp", strings.TrimPrefix(address, "http://"))
	if err != nil {
		t.Fatal("HTTP listener still open", err)
	}
	listener.Close()
}

func TestMacServiceRetriesConfigurationFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.json", []byte(`{"addr":"0.0.0.0:0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	h := newTestHost(t, "config.json")
	h.status.wait(t, "● 服务异常")
	h.open <- struct{}{}
	// Wait until the worker consumes this failed-state command before queuing
	// restart. Otherwise select may process restart first, legitimately opening
	// the now-ready page for the still-pending explicit open command.
	deadline := time.After(5 * time.Second)
	for len(h.open) != 0 {
		select {
		case <-deadline:
			t.Fatal("worker did not consume open request")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-h.browser:
		t.Fatal("opened unready service")
	default:
	}
	writeTestConfig(t, map[string]any{})
	h.restart <- struct{}{}
	h.status.wait(t, "● 服务运行中")
	select {
	case <-h.browser:
		t.Fatal("restart unexpectedly opened browser")
	default:
	}
	h.open <- struct{}{}
	if h.browserServer(t).URL() == "" {
		t.Fatal("no ready URL after retry")
	}
}

func TestMacServiceRetriesOccupiedPort(t *testing.T) {
	t.Chdir(t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()
	writeTestConfig(t, map[string]any{"addr": addr})
	h := newTestHost(t, "config.json")
	h.status.wait(t, "● 服务异常")
	select {
	case <-h.browser:
		t.Fatal("opened occupied port")
	default:
	}
	listener.Close()
	h.restart <- struct{}{}
	h.status.wait(t, "● 服务运行中")
	h.open <- struct{}{}
	if got := h.browserServer(t).URL(); got != "http://"+addr {
		t.Fatal("retry URL", got)
	}
}

func TestMacServiceCancellationBeforeStartup(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status := make(testStatus, 4)
	runService(ctx, "", slog.New(slog.NewJSONHandler(io.Discard, nil)), status, make(chan struct{}, 1), make(chan struct{}, 1), make(chan *app.HTTP, 1), nil, serviceOptions{})
	if _, err := os.Stat("data"); !os.IsNotExist(err) {
		t.Fatal("cancelled launch touched data", err)
	}
	status.wait(t, "● 服务已停止")
}
