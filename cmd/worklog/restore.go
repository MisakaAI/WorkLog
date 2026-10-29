package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"worklog/internal/diagnostics"
	"worklog/internal/storage"
)

func restore(ctx context.Context, source, destination string) error {
	return restoreWithIO(ctx, source, destination, os.Stdin, os.Stderr, time.Now)
}

func restoreWithIO(ctx context.Context, source, destination string, input io.Reader, output io.Writer, now func() time.Time) error {
	src, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	dst, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if src == dst {
		return diagnostics.Safe("source and destination must differ")
	}
	original, err := os.Lstat(dst)
	if errors.Is(err, os.ErrNotExist) {
		if err = checkRestoreSidecars(dst); err != nil {
			return err
		}
		// Keep the noninteractive, exclusive-create storage contract for new targets.
		return storage.RestoreSQLite(ctx, src, dst)
	}
	if err != nil {
		return err
	}
	backup := dst + "." + now().Format("20060102") + ".bak"
	if err = checkRestoreReplacement(src, dst, backup, original); err != nil {
		return err
	}
	// Validate before moving anything, not just during the eventual copy.
	if err = storage.VerifySQLiteRestoreSource(ctx, src); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(output, "Stop the service before restoring; do not manually delete WAL files.\nRetain %q as %q and restore from %q? [y/N]: ", dst, backup, src); err != nil {
		return err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return errors.Join(diagnostics.Safe("restore cancelled; destination unchanged"), err)
	}
	answer = strings.TrimSuffix(strings.TrimSuffix(answer, "\n"), "\r")
	if answer != "Y" && answer != "y" {
		return diagnostics.Safe("restore cancelled; destination unchanged")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// The user may have taken time to answer. Recheck identity and all conflicts.
	// This is not a lock: database processes must remain stopped throughout.
	if err = checkRestoreReplacement(src, dst, backup, original); err != nil {
		return err
	}
	// Same-directory hard linking cannot overwrite a competing backup. Never
	// fall back to Rename, which can overwrite, or delete any WAL/SHM files.
	if err = os.Link(dst, backup); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(output, "\nOld database retained at %q.\n", backup); err != nil {
		return err // Both names still exist; leave them intact.
	}
	if err = os.Remove(dst); err != nil {
		return err // The original and the backup remain; do not clean up either.
	}
	if err = storage.RestoreSQLite(ctx, src, dst); err != nil {
		_, outputErr := fmt.Fprintf(output, "Restore failed; old database remains at %q. No automatic rollback was performed.\n", backup)
		return errors.Join(err, outputErr)
	}
	return nil
}

func checkRestoreSidecars(destination string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(destination + suffix); err == nil {
			return diagnostics.Safe("destination WAL/SHM exists; stop the service and let SQLite checkpoint cleanly; do not manually delete WAL files")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func checkRestoreReplacement(source, destination, backup string, original os.FileInfo) error {
	current, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() {
		return diagnostics.Safe("restore destination must be a regular file, not a symbolic link")
	}
	if !os.SameFile(original, current) {
		return diagnostics.Safe("restore destination changed during confirmation; retry with the service stopped")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	if os.SameFile(sourceInfo, current) {
		return diagnostics.Safe("source and destination must differ")
	}
	if err = checkRestoreSidecars(destination); err != nil {
		return err
	}
	for _, path := range []string{backup, backup + "-wal", backup + "-shm"} {
		if _, err := os.Lstat(path); err == nil {
			return diagnostics.Safe("dated backup or its WAL/SHM already exists; refusing to overwrite")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
