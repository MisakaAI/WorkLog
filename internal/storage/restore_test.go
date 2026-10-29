package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func standaloneRestoreSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	u := sqliteFileURL(path)
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("CREATE TABLE restored_data (value TEXT); INSERT INTO restored_data VALUES ('restore me')"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRestoreTargetAbsent(t *testing.T, destination string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(destination + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("destination%s remains: %v", suffix, err)
		}
	}
}

func TestRestoreSQLiteStandaloneData(t *testing.T) {
	source := standaloneRestoreSource(t)
	destination := filepath.Join(t.TempDir(), "nested", "restored.db")
	if err := RestoreSQLite(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	if err := VerifySQLite(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	u := sqliteFileURL(destination)
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM restored_data").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "restore me" {
		t.Fatalf("restored value = %q", value)
	}
}

func TestRestoreSQLiteRefusesExistingFiles(t *testing.T) {
	source := standaloneRestoreSource(t)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		t.Run("destination"+suffix, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "restored.db")
			existing := destination + suffix
			const contents = "do not overwrite"
			if err := os.WriteFile(existing, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if err := RestoreSQLite(context.Background(), source, destination); err == nil {
				t.Fatal("accepted existing destination file")
			}
			got, err := os.ReadFile(existing)
			if err != nil || string(got) != contents {
				t.Fatalf("existing file changed: %q, %v", got, err)
			}
			if suffix != "" {
				if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("destination created: %v", err)
				}
			}
		})
	}
}

func TestRestoreSQLiteInvalidSourceLeavesNoDestination(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.db")
			if kind == "corrupt" {
				if err := os.WriteFile(source, []byte("not SQLite"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(dir, "restored.db")
			if err := RestoreSQLite(context.Background(), source, destination); err == nil {
				t.Fatal("accepted invalid source")
			}
			assertRestoreTargetAbsent(t, destination)
		})
	}
}

func TestRestoreSQLiteRefusesSourceWAL(t *testing.T) {
	source := standaloneRestoreSource(t)
	if err := os.WriteFile(source+"-wal", []byte("uncheckpointed data"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored.db")
	if err := RestoreSQLite(context.Background(), source, destination); err == nil {
		t.Fatal("accepted nonempty source WAL")
	}
	assertRestoreTargetAbsent(t, destination)
}

func TestVerifySQLiteRestoreSourceWAL(t *testing.T) {
	for _, kind := range []string{"absent", "empty", "nonempty", "directory"} {
		t.Run(kind, func(t *testing.T) {
			source := standaloneRestoreSource(t)
			var err error
			switch kind {
			case "empty":
				err = os.WriteFile(source+"-wal", nil, 0600)
			case "nonempty":
				err = os.WriteFile(source+"-wal", []byte("pending data"), 0600)
			case "directory":
				err = os.Mkdir(source+"-wal", 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = VerifySQLiteRestoreSource(context.Background(), source)
			wantValid := kind == "absent" || kind == "empty"
			if (err == nil) != wantValid {
				t.Fatalf("source WAL %s: err = %v, wantValid = %v", kind, err, wantValid)
			}
			if kind == "nonempty" {
				contents, err := os.ReadFile(source + "-wal")
				if err != nil || string(contents) != "pending data" {
					t.Fatalf("source WAL changed: %q, %v", contents, err)
				}
			}
		})
	}
}

func TestRestoreSQLiteRefusesNonregularSource(t *testing.T) {
	for _, kind := range []string{"symlink", "dangling-symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			actual := standaloneRestoreSource(t)
			source := filepath.Join(t.TempDir(), "source.db")
			var err error
			if kind == "directory" {
				err = os.Mkdir(source, 0700)
			} else {
				referent := actual
				if kind == "dangling-symlink" {
					referent += ".missing"
				}
				err = os.Symlink(referent, source)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = VerifySQLiteRestoreSource(context.Background(), source); err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
				t.Fatalf("preflight accepted nonregular source: %v", err)
			}
			destination := filepath.Join(t.TempDir(), "restored.db")
			// The copy entry point must enforce the same check, not just the CLI.
			if err = RestoreSQLite(context.Background(), source, destination); err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
				t.Fatalf("restore accepted nonregular source: %v", err)
			}
			assertRestoreTargetAbsent(t, destination)
		})
	}
}

func TestRestoreSQLiteSourceParentSymlink(t *testing.T) {
	for _, wal := range []bool{false, true} {
		name := "standalone"
		if wal {
			name = "nonempty-wal"
		}
		t.Run(name, func(t *testing.T) {
			actual := standaloneRestoreSource(t)
			parent := filepath.Join(t.TempDir(), "source-directory")
			if err := os.Symlink(filepath.Dir(actual), parent); err != nil {
				t.Fatal(err)
			}
			if wal {
				if err := os.WriteFile(actual+"-wal", []byte("pending data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := filepath.Join(parent, filepath.Base(actual))
			destination := filepath.Join(t.TempDir(), "restored.db")
			err := RestoreSQLite(context.Background(), source, destination)
			if wal {
				if err == nil || !strings.Contains(err.Error(), "standalone backup") {
					t.Fatalf("parent alias bypassed source WAL check: %v", err)
				}
				assertRestoreTargetAbsent(t, destination)
			} else if err != nil {
				t.Fatalf("parent symlink should allow standalone source: %v", err)
			}
		})
	}
}

func TestRestoreSQLiteRefusesSamePath(t *testing.T) {
	source := standaloneRestoreSource(t)
	if err := RestoreSQLite(context.Background(), source, source); err == nil {
		t.Fatal("accepted identical source and destination")
	}
	if err := VerifySQLite(context.Background(), source); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}
