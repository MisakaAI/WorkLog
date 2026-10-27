package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStaticRootCannotContainPrivatePaths(t *testing.T) {
	dir := t.TempDir()
	c := Config{Driver: "sqlite", DSN: filepath.Join(dir, "data", "worklog.db"), TokenFile: filepath.Join(dir, "data", "token"), StaticDir: dir}
	if validateStaticDir(c) == nil {
		t.Fatal("private ancestor accepted")
	}
	c.StaticDir = filepath.Join(dir, "web")
	if err := validateStaticDir(c); err != nil {
		t.Fatal(err)
	}
	mustMkdir := func(path string) {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir(filepath.Join(dir, "data"))
	link := filepath.Join(dir, "static-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	c.StaticDir = link
	if validateStaticDir(c) == nil {
		t.Fatal("symlinked ancestor accepted")
	}
	c.StaticDir = "."
	if validateStaticDir(c) == nil {
		t.Fatal("source root accepted")
	}
	c.StaticDir = ""
	if err := validateStaticDir(c); err != nil {
		t.Fatal(err)
	}
}
