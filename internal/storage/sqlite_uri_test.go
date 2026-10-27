package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSQLiteFileURL(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
	}{
		{"unix", "/home/user/worklog.db", "file:///home/user/worklog.db"},
		{"windows drive", "C:/Users/Misaka/worklog.db", "file:///C:/Users/Misaka/worklog.db"},
		{"windows escaping", "D:/Work log/工作记录#100%.db", "file:///D:/Work%20log/%E5%B7%A5%E4%BD%9C%E8%AE%B0%E5%BD%95%23100%25.db"},
		{"unix escaping", "/data/Work log/#100%.db", "file:///data/Work%20log/%23100%25.db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := sqliteFileURL(tc.path)
			if got := u.String(); got != tc.want {
				t.Fatalf("URI = %q, want %q", got, tc.want)
			}
			if u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
				t.Fatalf("path interpreted as URI metadata: %+v", u)
			}
		})
	}
	if runtime.GOOS == "windows" {
		u := sqliteFileURL(`C:\Users\Misaka\Work log\工作记录.db`)
		if want := "file:///C:/Users/Misaka/Work%20log/%E5%B7%A5%E4%BD%9C%E8%AE%B0%E5%BD%95.db"; u.String() != want {
			t.Fatalf("Windows URI = %q, want %q", u.String(), want)
		}
	}
}

func TestSQLiteLocalPaths(t *testing.T) {
	for _, name := range []string{"plain", "空格 Work log #100% & +'"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), name, "工作记录 #100%.db")
			store, err := Open(ctx, "sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.DB.ExecContext(ctx, "CREATE TABLE path_probe (value TEXT); INSERT INTO path_probe VALUES ('persisted')"); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(filepath.Dir(path), "备份 #100%.db")
			if err := store.Backup(ctx, backup); err != nil {
				t.Fatal(err)
			}
			if err := VerifySQLite(ctx, backup); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := VerifySQLite(ctx, path); err != nil {
				t.Fatal(err)
			}
			for _, dbPath := range []string{path, backup} {
				reopened, err := Open(ctx, "sqlite", dbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				var value string
				if err := reopened.DB.QueryRowContext(ctx, "SELECT value FROM path_probe").Scan(&value); err != nil || value != "persisted" {
					t.Fatalf("data not persisted: value=%q, err=%v", value, err)
				}
			}
		})
	}
}

func TestVerifySQLiteRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	path := filepath.Join("relative #100%", "worklog.db")
	store, err := Open(ctx, "sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifySQLite(ctx, path); err != nil {
		t.Fatal(err)
	}
	missing := "missing.db"
	if err := VerifySQLite(ctx, missing); err == nil {
		t.Fatal("verification accepted a missing database")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verification created a missing database: %v", err)
	}
}
