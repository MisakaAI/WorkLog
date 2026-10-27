package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"worklog/internal/diagnostics"
)

type Config struct {
	Addr           string   `json:"addr"`
	Driver         string   `json:"db_driver"`
	DSN            string   `json:"db_dsn"`
	User           string   `json:"user_id"`
	Timezone       string   `json:"timezone"`
	StaticDir      string   `json:"static_dir"`
	TokenFile      string   `json:"token_file"`
	WriteToken     string   `json:"write_token"`
	ReadToken      string   `json:"read_token"`
	BasicUser      string   `json:"basic_user"`
	BasicPassword  string   `json:"basic_password"`
	AllowedHosts   []string `json:"allowed_hosts"`
	AllowedOrigins []string `json:"allowed_origins"`
	MCPDestructive bool     `json:"mcp_allow_destructive"`
	StdioReadOnly  bool     `json:"stdio_read_only"`
	Local          bool     `json:"-"`
	configFile     string
}

// Load uses only built-in defaults, without reading business environment variables
// or discovering a configuration file in the working directory.
func Load() (Config, error) {
	return loadConfig("", false)
}

// LoadFile 只使用指定 JSON 文件与内置默认值，不读取环境变量。
// 传入目录时读取其中的 config.json；相对路径仍以进程工作目录为基准。
func LoadFile(path string) (Config, error) {
	if path == "" {
		return Config{}, diagnostics.Safe("configuration path must not be empty")
	}
	return loadConfig(path, false)
}

// LoadDesktop uses per-user desktop defaults while preserving the existing JSON
// schema and authentication settings. The caller sets its working directory to
// the data directory so explicitly configured relative paths remain predictable.
func LoadDesktop(path string) (Config, error) {
	return loadConfig(path, true)
}

func loadConfig(path string, desktop bool) (Config, error) {
	c := Config{
		Addr: "127.0.0.1:12450", Driver: "sqlite", DSN: "./data/worklog.db",
		User: "owner", Timezone: "Asia/Shanghai", StaticDir: "Web/dist",
		BasicUser: "worklog", TokenFile: "./data/mcp-token",
		AllowedHosts:   []string{"localhost", "127.0.0.1", "::1"},
		AllowedOrigins: []string{"http://localhost:5173", "http://127.0.0.1:5173"},
	}
	if desktop {
		c.Addr, c.StaticDir, c.AllowedOrigins = "127.0.0.1:0", "", []string{}
	}
	if path != "" {
		if err := readFile(path, &c); err != nil {
			return c, err
		}
	}
	host, _, e := net.SplitHostPort(c.Addr)
	if e != nil {
		return c, diagnostics.Safe("addr must be host:port")
	}
	if desktop {
		if host != "127.0.0.1" {
			return c, diagnostics.Safe("desktop addr must bind to 127.0.0.1")
		}
		// Desktop always serves embedded resources, never user data or disk files.
		c.StaticDir = ""
	}
	ip := net.ParseIP(host)
	c.Local = host == "localhost" || ip != nil && ip.IsLoopback()
	if _, e = time.LoadLocation(c.Timezone); e != nil {
		return c, diagnostics.Safe("invalid timezone")
	}
	if c.User == "" || len(c.User) > 255 {
		return c, diagnostics.Safe("invalid user_id")
	}
	if c.Driver != "sqlite" && c.Driver != "postgres" {
		return c, diagnostics.Safe("db_driver must be sqlite or postgres")
	}
	if c.Driver == "postgres" && (strings.TrimSpace(c.DSN) == "" || c.DSN == "./data/worklog.db") {
		return c, diagnostics.Safe("PostgreSQL requires db_dsn")
	}
	if c.BasicPassword != "" && len(c.BasicPassword) < 8 {
		return c, diagnostics.Safe("basic_password must be at least 8 bytes")
	}
	// 远程监听必须显式配置强凭证，不能沿用本机模式的免密访问条件。
	if !c.Local && (len(c.WriteToken) < 32 || len(c.BasicPassword) < 8) {
		return c, diagnostics.Safe("remote addr requires write_token (32+ bytes), basic_password (8+ bytes), explicit allowed_hosts/allowed_origins and HTTPS reverse proxy")
	}
	if err := validateStaticDir(c); err != nil {
		return c, err
	}
	if c.WriteToken == "" {
		tokenPath := c.TokenFile
		if e = os.MkdirAll(filepath.Dir(tokenPath), 0700); e != nil {
			return c, e
		}
		token, err := loadOrCreateToken(tokenPath)
		if err != nil {
			return c, err
		}
		c.WriteToken = token
	}
	if len(c.WriteToken) < 32 || c.ReadToken != "" && len(c.ReadToken) < 32 || c.ReadToken != "" && c.ReadToken == c.WriteToken {
		return c, diagnostics.Safe("write_token and read_token must be distinct and at least 32 bytes")
	}
	return c, nil
}
