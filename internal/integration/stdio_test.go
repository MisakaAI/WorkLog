package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"worklog/internal/service"
	"worklog/internal/storage"
)

func TestActualStdioCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("stdio subprocess build omitted in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "worklog")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/worklog")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	for _, tc := range []struct {
		name     string
		env      string
		file     bool
		readOnly bool
	}{
		{"env_writable", "false", false, false},
		{"env_read_only", "true", false, true},
		{"file_read_only_ignores_env", "false", true, true},
		{"file_writable_ignores_env", "true", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stdio.db")
			args := []string{"stdio"}
			if tc.file {
				conf := filepath.Join(dir, "config.json")
				data, e := json.Marshal(map[string]any{
					"db_dsn": path, "token_file": filepath.Join(dir, "token"),
					"static_dir": "", "stdio_read_only": tc.readOnly,
					"mcp_allow_destructive": true,
				})
				must(t, e)
				must(t, os.WriteFile(conf, data, 0600))
				args = []string{"--conf", conf, "stdio"}
			}
			command := exec.CommandContext(ctx, binary, args...)
			command.Env = append(os.Environ(), "WORKLOG_ADDR=127.0.0.1:12450", "WORKLOG_DB_DRIVER=sqlite", "WORKLOG_DB_DSN="+path, "WORKLOG_TOKEN_FILE="+filepath.Join(dir, "token"), "WORKLOG_WRITE_TOKEN=", "WORKLOG_READ_TOKEN=", "WORKLOG_STATIC_DIR=", "WORKLOG_MCP_ALLOW_DESTRUCTIVE=true", "WORKLOG_STDIO_READ_ONLY="+tc.env)
			client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
			session, e := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
			must(t, e)
			defer session.Close()
			res, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_todo", Arguments: map[string]any{"title": "stdio todo", "idempotencyKey": "stdio"}})
			must(t, e)
			if res.IsError != tc.readOnly {
				t.Fatalf("create_todo IsError = %v, want %v: %v", res.IsError, tc.readOnly, res.Content)
			}
			if tc.readOnly {
				if len(res.Content) != 1 {
					t.Fatalf("unexpected error content: %v", res.Content)
				}
				text, ok := res.Content[0].(*mcp.TextContent)
				if !ok {
					t.Fatalf("unexpected error content: %T", res.Content[0])
				}
				var payload struct {
					Error struct{ Code string }
				}
				must(t, json.Unmarshal([]byte(text.Text), &payload))
				if payload.Error.Code != "FORBIDDEN" {
					t.Fatalf("unexpected error: %s", text.Text)
				}
			}
			res, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_todos", Arguments: map[string]any{}})
			must(t, e)
			if res.IsError {
				t.Fatal(res.Content)
			}
			store, e := storage.Open(ctx, "sqlite", path)
			must(t, e)
			defer store.Close()
			d, e := service.New(store, time.UTC).Dashboard(ctx, owner)
			must(t, e)
			if tc.readOnly {
				if len(d.Todos) != 0 {
					t.Fatalf("read-only call wrote todos: %v", d.Todos)
				}
			} else if len(d.Todos) != 1 || d.Todos[0].Title != "stdio todo" {
				t.Fatal(d)
			}
		})
	}
}
