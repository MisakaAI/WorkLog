package storage

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSQLiteFileURLUsesPathNotAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "工作 日志 #%.db")
	u := sqliteFileURL(path)
	parsed, err := url.Parse(u.String())
	if err != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Fragment != "" {
		t.Fatalf("invalid SQLite file URI: %s (%v)", u.String(), err)
	}
	if !strings.HasSuffix(parsed.Path, "/工作 日志 #%.db") {
		t.Fatalf("lost filename: %s", u.String())
	}
	if runtime.GOOS == "windows" {
		windowsURL := sqliteFileURL(`C:\Users\测试\worklog.db`)
		got := windowsURL.String()
		want := "file:///C:/Users/%E6%B5%8B%E8%AF%95/worklog.db"
		if got != want {
			t.Fatalf("drive was treated as URI host: %s, want %s", got, want)
		}
	}
}
