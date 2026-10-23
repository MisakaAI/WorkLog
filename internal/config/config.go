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

func env(key, def string) string {
	if v, ok := os.LookupEnv("WORKLOG_" + key); ok {
		return v
	}
	return def
}
func split(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func Load() (Config, error) {
	return load("")
}

// LoadFile 只使用指定 JSON 文件与内置默认值，不读取环境变量。
// 传入目录时读取其中的 config.json；相对路径仍以进程工作目录为基准。
func LoadFile(path string) (Config, error) {
	if path == "" {
		return Config{}, diagnostics.Safe("configuration path must not be empty")
	}
	return load(path)
}

func load(path string) (Config, error) {
	get := env
	if path != "" {
		get = func(_ string, def string) string { return def }
	}
	c := Config{Addr: get("ADDR", "127.0.0.1:8080"), Driver: get("DB_DRIVER", "sqlite"), DSN: get("DB_DSN", "./data/worklog.db"), User: get("USER_ID", "owner"), Timezone: get("TIMEZONE", "Asia/Shanghai"), StaticDir: get("STATIC_DIR", "Web/dist"), WriteToken: get("WRITE_TOKEN", ""), ReadToken: get("READ_TOKEN", ""), BasicUser: get("BASIC_USER", "worklog"), BasicPassword: get("BASIC_PASSWORD", ""), AllowedHosts: split(get("ALLOWED_HOSTS", "localhost,127.0.0.1,::1")), AllowedOrigins: split(get("ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")), MCPDestructive: get("MCP_ALLOW_DESTRUCTIVE", "false") == "true", TokenFile: get("TOKEN_FILE", "./data/mcp-token"), StdioReadOnly: get("STDIO_READ_ONLY", "false") == "true"}
	if path != "" {
		if err := readFile(path, &c); err != nil {
			return c, err
		}
	}
	host, _, e := net.SplitHostPort(c.Addr)
	if e != nil {
		return c, diagnostics.Safe("WORKLOG_ADDR must be host:port")
	}
	ip := net.ParseIP(host)
	c.Local = host == "localhost" || ip != nil && ip.IsLoopback()
	if _, e = time.LoadLocation(c.Timezone); e != nil {
		return c, diagnostics.Safe("invalid business timezone")
	}
	if c.User == "" || len(c.User) > 255 {
		return c, diagnostics.Safe("invalid server identity")
	}
	if c.Driver != "sqlite" && c.Driver != "postgres" {
		return c, diagnostics.Safe("DB_DRIVER must be sqlite or postgres")
	}
	if c.Driver == "postgres" && c.DSN == "./data/worklog.db" {
		return c, diagnostics.Safe("PostgreSQL requires DB_DSN")
	}
	if c.BasicPassword != "" && len(c.BasicPassword) < 8 {
		return c, diagnostics.Safe("BASIC_PASSWORD must be at least 8 bytes")
	}
	// 远程监听必须显式配置强凭证，不能沿用本机模式的免密访问条件。
	if !c.Local && (len(c.WriteToken) < 32 || len(c.BasicPassword) < 8) {
		return c, diagnostics.Safe("remote bind requires WRITE_TOKEN (32+ bytes), BASIC_PASSWORD (8+ bytes), explicit trusted hosts/origins and HTTPS reverse proxy")
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
		return c, diagnostics.Safe("tokens must be distinct and at least 32 bytes")
	}
	return c, nil
}
