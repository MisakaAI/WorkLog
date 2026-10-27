package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrCreateTokenReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatalf("generated token length = %d, want 64", len(token))
	}
	reloaded, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != token {
		t.Fatal("reloading changed the generated token")
	}
}

func TestLoadOrCreateTokenRejectsNonRegular(t *testing.T) {
	if _, err := loadOrCreateToken(t.TempDir()); err == nil {
		t.Fatal("directory accepted as a token file")
	}
}

func TestLoadOrCreateTokenPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0400, 0640, 0604, 0644, 0666, 0444} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			want := strings.Repeat("a", 64)
			if err := os.WriteFile(path, []byte(want), 0600); err != nil {
				t.Fatal(err)
			}
			// Set the exact mode without the process umask masking insecure bits.
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0600) })
			token, err := loadOrCreateToken(path)
			wantError := runtime.GOOS != "windows" && mode&0077 != 0
			if (err != nil) != wantError {
				t.Fatalf("mode %04o: error = %v, wantError = %v", mode, err, wantError)
			}
			if !wantError && token != want {
				t.Fatal("existing token changed")
			}
		})
	}
}

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
