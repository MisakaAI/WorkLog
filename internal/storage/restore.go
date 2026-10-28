package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"worklog/internal/diagnostics"
)

// RestoreSQLite copies a verified standalone SQLite backup to a new database.
// The destination database and its WAL/SHM files must not exist. Callers must
// stop database processes and move aside all three files before restoring.
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
	if info, err := os.Stat(src + "-wal"); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 0 {
			return diagnostics.Safe("restore requires a standalone backup, not a live WAL database")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = VerifySQLite(ctx, src); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	// 独占创建目标，避免覆盖现有数据。恢复前必须停止所有写入进程，
	// 并将旧数据库及对应的 WAL/SHM 文件一并移走。
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
