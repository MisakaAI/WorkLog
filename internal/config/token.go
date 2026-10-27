package config

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"worklog/internal/diagnostics"
)

func loadOrCreateToken(path string) (string, error) {
	// 其他进程可能刚创建文件、尚未完成写入。短暂等待其发布，
	// 绝不替换已有 Token，以免正在运行的客户端凭证失效。
	for attempt := 0; attempt < 100; attempt++ {
		file, err := os.Open(path)
		if err == nil {
			// Check the same open file that we read, before reading credentials.
			b, e := readTokenFile(file)
			closeErr := file.Close()
			if e != nil {
				return "", e
			}
			if closeErr != nil {
				return "", closeErr
			}
			token := strings.TrimSpace(string(b))
			if len(token) >= 32 {
				return token, nil
			}
		} else if os.IsNotExist(err) {
			random := make([]byte, 32)
			if _, err = rand.Read(random); err != nil {
				return "", err
			}
			token := hex.EncodeToString(random)
			// Atomically publish a complete, private file without replacing a
			// competing token. Rename can overwrite; hard-link publication cannot.
			e := publishToken(path, token)
			if e == nil {
				return token, nil
			}
			if !os.IsExist(e) {
				return "", e
			}
		} else {
			return "", err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", diagnostics.Safe("token file is incomplete or invalid; check token generation and retry")
}

func readTokenFile(file *os.File) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, diagnostics.Safe("token file must be regular and private")
	}
	if err := checkTokenPermissions(file, info); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}

func publishToken(path, token string) error {
	file, err := createPrivateTokenTemp(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(token)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Do not fall back to rename or copying on filesystems (including some
	// remote shares) without hard links: that would weaken no-replace publication.
	return os.Link(file.Name(), path)
}
