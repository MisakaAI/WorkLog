package config

import (
	"os"
	"path/filepath"
	"strings"

	"worklog/internal/diagnostics"
)

// validateStaticDir 防止将源码根目录或包含敏感文件的目录作为静态资源公开。
func validateStaticDir(c Config) error {
	if c.StaticDir == "" {
		return nil
	}
	root, err := canonicalPath(c.StaticDir)
	if err != nil {
		return diagnostics.Wrap("resolve_static_directory", err)
	}
	if filepath.Dir(root) == root {
		return diagnostics.Safe("STATIC_DIR must be a dedicated front-end build directory")
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return diagnostics.Safe("STATIC_DIR must not be the source root")
	}
	tokenFile := c.TokenFile
	if tokenFile == "" {
		tokenFile = "./data/mcp-token"
	}
	sensitive := []string{tokenFile, "./backups"}
	if c.configFile != "" {
		sensitive = append(sensitive, c.configFile)
	}
	if c.Driver == "sqlite" {
		sensitive = append(sensitive, c.DSN)
	}
	for _, path := range sensitive {
		path, err := canonicalPath(path)
		if err != nil {
			return diagnostics.Wrap("resolve_private_path", err)
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return diagnostics.Safe("STATIC_DIR must not contain database, token, configuration or backup paths")
		}
	}
	return nil
}

// canonicalPath 即使目标尚未创建，也会逐级解析已有父目录中的符号链接。
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", err
	}
	resolved, err = canonicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}
