package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopDefaultsIgnoreEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	poisonBusinessEnvironment(t)
	c, err := LoadDesktop("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:0" || !c.Local || c.StaticDir != "" || len(c.AllowedOrigins) != 0 || c.Driver != "sqlite" || c.DSN != "./data/worklog.db" || c.User != "owner" || c.Timezone != "Asia/Shanghai" || c.TokenFile != "./data/mcp-token" || len(c.WriteToken) != 64 || c.ReadToken != "" || c.BasicUser != "worklog" || c.BasicPassword != "" || c.MCPDestructive || c.StdioReadOnly || strings.Join(c.AllowedHosts, ",") != "localhost,127.0.0.1,::1" {
		t.Fatal("incorrect desktop defaults")
	}
}

func TestDesktopDefaultsAndExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"write_token":"`+strings.Repeat("w", 32)+`","basic_password":"keep-password","db_dsn":"existing.db"}`), 0600); err != nil {
		t.Fatal(err)
	}
	poisonBusinessEnvironment(t)
	c, err := LoadDesktop(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:0" || !c.Local || c.StaticDir != "" || len(c.AllowedOrigins) != 0 {
		t.Fatalf("incorrect desktop defaults: %#v", c)
	}
	if c.DSN != "existing.db" || c.BasicPassword != "keep-password" {
		t.Fatal("existing settings changed")
	}
}

func TestDesktopRejectsRemoteBind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":"0.0.0.0:12450","write_token":"`+strings.Repeat("w", 32)+`","basic_password":"keep-password"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDesktop(path); err == nil {
		t.Fatal("desktop accepted non-loopback listener")
	}
}
