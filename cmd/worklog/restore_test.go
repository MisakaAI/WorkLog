package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var restoreTestTime = time.Date(2026, 10, 29, 23, 59, 0, 0, time.FixedZone("test", 8*60*60))

const oldRestoreContents = "old database to retain"

func restoreFixture(t *testing.T) (source, target, backup string) {
	t.Helper()
	dir := t.TempDir()
	source = filepath.Join(dir, "source.db")
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(source)}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := db.Exec("CREATE TABLE restored_data (value TEXT); INSERT INTO restored_data VALUES ('restored')")
	if err = errors.Join(execErr, db.Close()); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(dir, "target.db")
	writeRestoreFile(t, target, oldRestoreContents)
	return source, target, target + ".20261029.bak"
}

func writeRestoreFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertRestoreContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s contents = %q, err = %v; want %q", path, got, err, want)
	}
}

func assertRestoreAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist: %v", path, err)
	}
}

func runRestoreTest(source, target string, input io.Reader, output io.Writer) error {
	return restoreWithIO(context.Background(), source, target, input, output, func() time.Time { return restoreTestTime })
}

func TestRestoreReplacementConfirmation(t *testing.T) {
	for _, answer := range []string{"Y\n", "y\n", "y\r\n", "\n", "", "Y", "y", "n\n", "yes\n", "YY\n", "Y extra\n", " y\n", "y \n", "\nY\n"} {
		t.Run(strings.ReplaceAll(answer, "\n", "newline"), func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			want, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err = runRestoreTest(source, target, strings.NewReader(answer), &output)
			confirmed := answer == "Y\n" || answer == "y\n" || answer == "y\r\n"
			if confirmed {
				if err != nil {
					t.Fatal(err)
				}
				assertRestoreContents(t, target, string(want))
				assertRestoreContents(t, backup, oldRestoreContents)
				if !strings.Contains(output.String(), "Old database retained at") || !strings.Contains(output.String(), backup) {
					t.Fatalf("missing retention notice: %q", output.String())
				}
			} else {
				if err == nil {
					t.Fatal("cancellation must return an error (nonzero CLI exit)")
				}
				assertRestoreContents(t, target, oldRestoreContents)
				assertRestoreAbsent(t, backup)
			}
			if !strings.Contains(output.String(), "[y/N]") {
				t.Fatalf("missing default-no prompt: %q", output.String())
			}
		})
	}
}

type restoreReaderFunc func([]byte) (int, error)

func (f restoreReaderFunc) Read(p []byte) (int, error) { return f(p) }

type restoreWriterFunc func([]byte) (int, error)

func (f restoreWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestRestoreEmptySidecarsAreNotRemoved(t *testing.T) {
	for _, targetExists := range []bool{true, false} {
		for _, suffix := range []string{"-wal", "-shm"} {
			source, target, backup := restoreFixture(t)
			if !targetExists {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			writeRestoreFile(t, target+suffix, "")
			var output bytes.Buffer
			err := runRestoreTest(source, target, strings.NewReader("Y\n"), &output)
			if err == nil || !strings.Contains(err.Error(), "do not manually delete WAL") {
				t.Fatalf("missing empty sidecar refusal: %v", err)
			}
			if output.Len() != 0 {
				t.Fatalf("unexpected prompt: %q", output.String())
			}
			assertRestoreContents(t, target+suffix, "")
			assertRestoreAbsent(t, backup)
			if targetExists {
				assertRestoreContents(t, target, oldRestoreContents)
			} else {
				assertRestoreAbsent(t, target)
			}
		}
	}
}

func TestRestoreIOFailuresLeaveOriginal(t *testing.T) {
	for _, step := range []string{"input", "prompt", "retention-notice"} {
		t.Run(step, func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			failure := errors.New("injected IO failure")
			input := restoreReaderFunc(func(p []byte) (int, error) {
				if step == "input" {
					return 0, failure
				}
				return copy(p, "Y\n"), nil
			})
			output := restoreWriterFunc(func(p []byte) (int, error) {
				if step == "prompt" || step == "retention-notice" && strings.Contains(string(p), "Old database retained at") {
					return 0, failure
				}
				return len(p), nil
			})
			if err := runRestoreTest(source, target, input, output); !errors.Is(err, failure) {
				t.Fatalf("expected IO failure: %v", err)
			}
			assertRestoreContents(t, target, oldRestoreContents)
			if step == "retention-notice" {
				assertRestoreContents(t, backup, oldRestoreContents)
			} else {
				assertRestoreAbsent(t, backup)
			}
		})
	}
}

func TestRestoreNewTargetDoesNotPrompt(t *testing.T) {
	source, target, backup := restoreFixture(t)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	input := restoreReaderFunc(func([]byte) (int, error) {
		t.Fatal("new target must not read confirmation")
		return 0, io.EOF
	})
	if err := runRestoreTest(source, target, input, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected output: %s", &output)
	}
	assertRestoreAbsent(t, backup)
}

func TestRestoreReplacementPreflight(t *testing.T) {
	for _, kind := range []string{"missing-source", "corrupt-source", "source-wal", "source-wal-directory", "target-wal", "target-shm", "target-dangling-wal", "backup", "backup-wal", "backup-shm", "backup-dangling", "same-path", "relative-alias", "hardlink-source", "symlink-source"} {
		t.Run(kind, func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			var conflict string
			switch kind {
			case "missing-source":
				source += ".missing"
			case "corrupt-source":
				writeRestoreFile(t, source, "invalid SQLite")
			case "source-wal":
				conflict = source + "-wal"
			case "source-wal-directory":
				if err := os.Mkdir(source+"-wal", 0700); err != nil {
					t.Fatal(err)
				}
			case "target-wal", "target-shm":
				conflict = target + strings.TrimPrefix(kind, "target")
			case "backup", "backup-wal", "backup-shm":
				conflict = backup + strings.TrimPrefix(kind, "backup")
			case "target-dangling-wal", "backup-dangling":
				path := target + "-wal"
				if kind == "backup-dangling" {
					path = backup
				}
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			case "same-path":
				source = target
			case "relative-alias":
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				source, err = filepath.Rel(cwd, target)
				if err != nil {
					t.Fatal(err)
				}
			case "hardlink-source", "symlink-source":
				source += ".alias"
				link := os.Link
				if kind == "symlink-source" {
					link = os.Symlink
				}
				if err := link(target, source); err != nil {
					t.Fatal(err)
				}
			}
			if conflict != "" {
				writeRestoreFile(t, conflict, "do not change")
			}
			var output bytes.Buffer
			err := runRestoreTest(source, target, strings.NewReader("Y\n"), &output)
			if err == nil {
				t.Fatal("accepted unsafe replacement")
			}
			if output.Len() != 0 {
				t.Fatalf("preflight should reject before prompting: %q", output.String())
			}
			if (kind == "same-path" || kind == "relative-alias" || kind == "hardlink-source" || kind == "symlink-source") && !strings.Contains(err.Error(), "source and destination must differ") {
				t.Fatalf("alias not rejected by identity check: %v", err)
			}
			if strings.HasPrefix(kind, "target-") && (!strings.Contains(err.Error(), "stop the service") || !strings.Contains(err.Error(), "do not manually delete WAL")) {
				t.Fatalf("missing WAL guidance: %v", err)
			}
			assertRestoreContents(t, target, oldRestoreContents)
			if kind != "backup" && kind != "backup-dangling" {
				assertRestoreAbsent(t, backup)
			}
			if conflict != "" {
				assertRestoreContents(t, conflict, "do not change")
			}
		})
	}
}

func TestRestoreRejectsSymlinkSourceWithCommittedWALBeforeConfirmation(t *testing.T) {
	source, target, backup := restoreFixture(t)
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(source)}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var mode string
	if err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("enable WAL: mode=%q, err=%v", mode, err)
	}
	if _, err = db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	var busy, frames, checkpointed int
	if err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 {
		t.Fatalf("checkpoint old data: busy=%d, err=%v", busy, err)
	}
	oldMain, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE restored_data SET value = 'committed in WAL'"); err != nil {
		t.Fatal(err)
	}
	var value string
	if err = db.QueryRow("SELECT value FROM restored_data").Scan(&value); err != nil || value != "committed in WAL" {
		t.Fatalf("committed value=%q, err=%v", value, err)
	}
	// The update is committed but not checkpointed: copying the main file alone
	// would silently restore the old value. Keep the connection open throughout.
	assertRestoreContents(t, source, string(oldMain))
	before := make(map[string]string)
	for _, path := range []string{source, source + "-wal", source + "-shm", target} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(contents)
	}
	if len(before[source+"-wal"]) == 0 {
		t.Fatal("fixture must have a nonempty WAL")
	}
	alias := filepath.Join(filepath.Dir(source), "backup-link.db")
	if err = os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	assertRestoreAbsent(t, alias+"-wal")
	input := restoreReaderFunc(func([]byte) (int, error) {
		t.Fatal("symlink WAL source must be rejected before confirmation")
		return 0, io.EOF
	})
	var output bytes.Buffer
	err = runRestoreTest(alias, target, input, &output)
	if err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
		t.Fatalf("expected symlink source refusal, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected confirmation prompt: %q", output.String())
	}
	assertRestoreAbsent(t, backup)
	for path, contents := range before {
		assertRestoreContents(t, path, contents)
	}
	if referent, err := os.Readlink(alias); err != nil || referent != source {
		t.Fatalf("source symlink changed: %q, %v", referent, err)
	}
	assertRestoreAbsent(t, alias+"-wal")
	assertRestoreAbsent(t, alias+"-shm")
}

func TestRestoreReplacementRejectsNonregularTarget(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "directory" {
				err = os.Mkdir(target, 0700)
			} else {
				referent := source
				if kind == "dangling-symlink" {
					referent += ".missing"
				}
				err = os.Symlink(referent, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if err = runRestoreTest(source, target, strings.NewReader("Y\n"), io.Discard); err == nil {
				t.Fatal("accepted nonregular target")
			}
			after, err := os.Lstat(target)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("target changed: %v", err)
			}
			assertRestoreAbsent(t, backup)
		})
	}
}

func TestRestoreReplacementRechecksAfterConfirmation(t *testing.T) {
	for _, change := range []string{"identity", "symlink", "target-wal", "target-shm", "backup", "backup-wal", "backup-shm", "source-alias"} {
		t.Run(change, func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			var conflict string
			input := restoreReaderFunc(func(p []byte) (int, error) {
				switch change {
				case "identity", "symlink":
					// Keep the old inode alive so the replacement cannot reuse it.
					if err := os.Rename(target, target+".original"); err != nil {
						t.Fatal(err)
					}
					if change == "identity" {
						writeRestoreFile(t, target, "replacement target")
					} else if err := os.Symlink(source, target); err != nil {
						t.Fatal(err)
					}
				case "source-alias":
					if err := os.Remove(source); err != nil {
						t.Fatal(err)
					}
					if err := os.Link(target, source); err != nil {
						t.Fatal(err)
					}
				default:
					if strings.HasPrefix(change, "target") {
						conflict = target + strings.TrimPrefix(change, "target")
					} else {
						conflict = backup + strings.TrimPrefix(change, "backup")
					}
					writeRestoreFile(t, conflict, "new conflict")
				}
				return copy(p, "Y\n"), nil
			})
			if err := runRestoreTest(source, target, input, io.Discard); err == nil {
				t.Fatal("accepted change during confirmation")
			}
			if change != "backup" {
				assertRestoreAbsent(t, backup)
			}
			if conflict != "" {
				assertRestoreContents(t, conflict, "new conflict")
			}
			switch change {
			case "identity":
				assertRestoreContents(t, target, "replacement target")
				assertRestoreContents(t, target+".original", oldRestoreContents)
			case "symlink":
				assertRestoreContents(t, target+".original", oldRestoreContents)
			default:
				assertRestoreContents(t, target, oldRestoreContents)
			}
		})
	}
}

func TestRestoreFailureAfterMoveRetainsBackup(t *testing.T) {
	source, target, backup := restoreFixture(t)
	// Remove the source only after the link is published: the post-confirmation
	// identity check also stats the source.
	input := strings.NewReader("Y\n")
	var output bytes.Buffer
	writer := restoreWriterFunc(func(p []byte) (int, error) {
		if strings.Contains(string(p), "Old database retained at") {
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
		}
		return output.Write(p)
	})
	if err := runRestoreTest(source, target, input, writer); err == nil {
		t.Fatal("missing source accepted after move")
	}
	assertRestoreContents(t, backup, oldRestoreContents)
	assertRestoreAbsent(t, target)
	if !strings.Contains(output.String(), "Restore failed; old database remains at") || !strings.Contains(output.String(), backup) || !strings.Contains(output.String(), "No automatic rollback") {
		t.Fatalf("missing failure/backup notice: %q", output.String())
	}
}

func TestRestoreMoveFailuresPreserveOriginal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions enforced for a non-root user")
	}
	for _, step := range []string{"link", "remove"} {
		t.Run(step, func(t *testing.T) {
			source, target, backup := restoreFixture(t)
			dir := filepath.Dir(target)
			defer os.Chmod(dir, 0700)
			denyWrites := func() {
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
			}
			input := restoreReaderFunc(func(p []byte) (int, error) {
				if step == "link" {
					denyWrites()
				}
				return copy(p, "Y\n"), nil
			})
			var output bytes.Buffer
			writer := restoreWriterFunc(func(p []byte) (int, error) {
				if step == "remove" && strings.Contains(string(p), "Old database retained at") {
					denyWrites()
				}
				return output.Write(p)
			})
			if err := runRestoreTest(source, target, input, writer); err == nil {
				t.Fatal("expected permission failure")
			}
			assertRestoreContents(t, target, oldRestoreContents)
			if step == "link" {
				assertRestoreAbsent(t, backup)
			} else {
				assertRestoreContents(t, backup, oldRestoreContents)
				if !strings.Contains(output.String(), backup) {
					t.Fatalf("missing backup path: %s", &output)
				}
			}
		})
	}
}
