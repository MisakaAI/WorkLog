package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"worklog/internal/diagnostics"
)

// VerifySQLiteRestoreSource requires a regular, nonsymlink standalone backup
// and rejects a nonempty or nonregular source WAL.
func VerifySQLiteRestoreSource(ctx context.Context, source string) error {
	// A file symlink can hide the referent's WAL from the source+"-wal" check:
	// SQLite may read that WAL, while io.Copy copies only the main database.
	// Parent directory symlinks preserve the database/sidecar name mapping.
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return diagnostics.Safe("restore source must be a regular file, not a symbolic link")
	}
	if info, err := os.Stat(source + "-wal"); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 0 {
			return diagnostics.Safe("restore requires a standalone backup, not a live WAL database")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return VerifySQLite(ctx, source)
}

// RestoreSQLite copies a verified standalone SQLite backup to a new database.
// The destination database and its WAL/SHM files must not exist. Callers must
// stop database processes and let SQLite checkpoint cleanly before retaining
// an old database. Never manually move or delete WAL/SHM to bypass these checks.
func RestoreSQLite(ctx context.Context, source, destination string) error {
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
	if err = VerifySQLiteRestoreSource(ctx, src); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	// 独占创建目标，避免覆盖现有数据。恢复前必须停服并由 SQLite 正常完成
	// checkpoint；不得手动移走或删除 WAL/SHM 来绕过检查。
	for _, p := range []string{dst, dst + "-wal", dst + "-shm"} {
		if _, err = os.Lstat(p); err == nil {
			return diagnostics.Safe("destination database/WAL/SHM must not exist")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = VerifySQLite(ctx, dst); err != nil {
		return err
	}
	ok = true
	return nil
}
