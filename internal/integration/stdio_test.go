package integration

import (
	"context"
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
	path := filepath.Join(dir, "stdio.db")
	command := exec.CommandContext(ctx, binary, "stdio")
	command.Env = append(os.Environ(), "WORKLOG_ADDR=127.0.0.1:8080", "WORKLOG_DB_DRIVER=sqlite", "WORKLOG_DB_DSN="+path, "WORKLOG_TOKEN_FILE="+filepath.Join(dir, "token"), "WORKLOG_WRITE_TOKEN=", "WORKLOG_READ_TOKEN=", "WORKLOG_MCP_ALLOW_DESTRUCTIVE=false", "WORKLOG_STDIO_READ_ONLY=false")
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	must(t, e)
	defer session.Close()
	res, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_todo", Arguments: map[string]any{"title": "stdio todo", "idempotencyKey": "stdio"}})
	must(t, e)
	if res.IsError {
		t.Fatal(res.Content)
	}
	store, e := storage.Open(ctx, "sqlite", path)
	must(t, e)
	defer store.Close()
	d, e := service.New(store, time.UTC).Dashboard(ctx, owner)
	must(t, e)
	if len(d.Todos) != 1 || d.Todos[0].Title != "stdio todo" {
		t.Fatal(d)
	}
}
