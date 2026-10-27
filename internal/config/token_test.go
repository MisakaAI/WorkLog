package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestExistingTokenPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	contents := "  " + strings.Repeat("existing-credential", 4) + "\n"
	if err := publishToken(path, contents); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := loadOrCreateToken(path)
		if err != nil || got != strings.TrimSpace(contents) {
			t.Fatalf("existing credentials changed: token=%q err=%v", got, err)
		}
	}
	assertTokenContents(t, path, contents)
}

func TestPublishTokenDoesNotReplaceExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	existing := strings.Repeat("a", 64)
	if err := publishToken(path, existing); err != nil {
		t.Fatal(err)
	}
	if err := publishToken(path, strings.Repeat("b", 64)); !os.IsExist(err) {
		t.Fatalf("expected an existing-file error, got %v", err)
	}
	assertTokenContents(t, path, existing)
	assertNoTokenTemps(t, filepath.Dir(path))
}

func TestInvalidExistingTokenNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := publishToken(path, "incomplete"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateToken(path); err == nil {
		t.Fatal("invalid existing token accepted")
	}
	assertTokenContents(t, path, "incomplete")
	assertNoTokenTemps(t, filepath.Dir(path))
}

func TestNonRegularTokenRejected(t *testing.T) {
	path := t.TempDir()
	if _, err := loadOrCreateToken(path); err == nil {
		t.Fatal("directory accepted as a token file")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("existing directory changed: %v", err)
	}
}

func TestInsecureExistingTokenNotChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	contents := strings.Repeat("existing", 8)
	if err := publishToken(path, contents); err != nil {
		t.Fatal(err)
	}
	if err := makeTokenInsecure(path); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := loadOrCreateToken(path); err == nil {
			t.Fatal("insecure existing token accepted or silently secured")
		}
	}
	assertTokenContents(t, path, contents)
	assertNoTokenTemps(t, filepath.Dir(path))
}

func assertTokenContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("token file changed: contents=%q err=%v", got, err)
	}
}

func assertNoTokenTemps(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, ".mcp-token-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary token files leaked: %v (err=%v)", files, err)
	}
}

func TestConcurrentFirstLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens", "mcp")
	conf := writeConfig(t, map[string]any{"token_file": path})
	const n = 32
	start := make(chan struct{})
	tokens := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { <-start; c, e := LoadFile(conf); tokens <- c.WriteToken; errs <- e })
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
	assertNoTokenTemps(t, filepath.Dir(path))
}
