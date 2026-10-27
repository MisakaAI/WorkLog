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
		file     bool
		readOnly bool
	}{
		{"defaults_writable_ignores_env", false, false},
		{"file_read_only_ignores_env", true, true},
		{"file_writable_ignores_env", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data", "worklog.db")
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
			command.Dir = dir
			// Conflicting legacy variables are regression inputs, never configuration.
			command.Env = append(os.Environ(), "WORKLOG_ADDR=invalid", "WORKLOG_DB_DRIVER=invalid", "WORKLOG_DB_DSN=ignored.db", "WORKLOG_TOKEN_FILE=ignored-token", "WORKLOG_WRITE_TOKEN=short", "WORKLOG_READ_TOKEN=short", "WORKLOG_TIMEZONE=invalid", "WORKLOG_USER_ID=ignored", "WORKLOG_MCP_ALLOW_DESTRUCTIVE=false", "WORKLOG_STDIO_READ_ONLY="+map[bool]string{true: "false", false: "true"}[tc.readOnly])
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
