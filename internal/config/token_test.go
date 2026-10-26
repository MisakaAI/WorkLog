package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentFirstLoad(t *testing.T) {
	t.Setenv("WORKLOG_ADDR", "127.0.0.1:12450")
	t.Setenv("WORKLOG_BASIC_PASSWORD", "")
	t.Setenv("WORKLOG_WRITE_TOKEN", "")
	t.Setenv("WORKLOG_READ_TOKEN", "")
	path := filepath.Join(t.TempDir(), "tokens", "mcp")
	t.Setenv("WORKLOG_TOKEN_FILE", path)
	const n = 32
	start := make(chan struct{})
	tokens := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { <-start; c, e := Load(); tokens <- c.WriteToken; errs <- e })
	}
	close(start)
	wg.Wait()
	close(tokens)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.TrimSpace(string(b))
	for token := range tokens {
		if token != expected || len(token) != 64 {
			t.Fatal("different or incomplete tokens")
		}
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".mcp-token-*"))
	if len(files) != 0 {
		t.Fatal("temporary token files leaked", files)
	}
}
