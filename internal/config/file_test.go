package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileIgnoresEnvironment(t *testing.T) {
	poisonBusinessEnvironment(t)
	t.Setenv("WORKLOG_STDIO_READ_ONLY", "false")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token := filepath.Join(dir, "private", "token")
	data, err := json.Marshal(map[string]any{
		"token_file": token, "static_dir": "", "stdio_read_only": true,
		"allowed_hosts": []string{"example.com"}, "allowed_origins": []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{dir, path} {
		c, err := LoadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		if c.Addr != "127.0.0.1:12450" || c.Driver != "sqlite" || c.Timezone != "Asia/Shanghai" || !c.Local || !c.StdioReadOnly || c.MCPDestructive || c.StaticDir != "" || c.TokenFile != token || len(c.WriteToken) != 64 || len(c.AllowedHosts) != 1 || len(c.AllowedOrigins) != 0 {
			t.Fatal("file settings/defaults not applied correctly")
		}
	}
	if info, err := os.Stat(token); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file-configured token not created securely", err)
	}
}

func TestLoadFileRejectsInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	for _, input := range []string{
		`{"unknown_secret":"do-not-log"}`, `{"addr":123}`, `{"stdio_read_only":"true"}`,
		`{`, `null`, `[]`, `{} {}`, `{} trailing`,
		`{"addr":"0.0.0.0:12450"}`, `{"timezone":"invalid"}`,
		`{"write_token":"short"}`, `{"db_driver":"other"}`,
	} {
		t.Run(input, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFile(path)
			if err == nil || strings.Contains(err.Error(), "do-not-log") {
				t.Fatal("invalid configuration accepted or secret leaked", err)
			}
		})
	}
	for _, path := range []string{"", filepath.Join(dir, "missing"), t.TempDir()} {
		if _, err := LoadFile(path); err == nil {
			t.Fatal("missing configuration accepted")
		}
	}
}

func TestFileConfigurationStaticPathProtection(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "dist")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	for _, token := range []string{filepath.Join(dir, "token"), filepath.Join(root, "token")} {
		data, _ := json.Marshal(map[string]any{"static_dir": root, "token_file": token, "write_token": strings.Repeat("w", 32)})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFile(path); err == nil {
			t.Fatal("static directory may contain configuration or token")
		}
	}
}
