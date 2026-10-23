package config

import (
	"crypto/rand"
	"encoding/hex"
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
		b, err := os.ReadFile(path)
		if err == nil {
			info, e := os.Stat(path)
			if e != nil {
				return "", e
			}
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return "", diagnostics.Safe("token file must be regular and private (chmod 600)")
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
			// 以 0600 权限原子发布完整文件，不覆盖其他进程先发布的 Token。
			// Unix 上普通 rename 会覆盖目标，因此改用硬链接发布。
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

func publishToken(path, token string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".mcp-token-*")
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
	return os.Link(file.Name(), path)
}
