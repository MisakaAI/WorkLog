package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, fields map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Every former business variable must be ignored, including when no file is supplied.
func poisonBusinessEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"ADDR": "bad-address", "DB_DRIVER": "invalid", "DB_DSN": "ignored.db",
		"USER_ID": "", "TIMEZONE": "invalid/place", "STATIC_DIR": ".",
		"TOKEN_FILE": "/not-used/token", "WRITE_TOKEN": "short", "READ_TOKEN": "short",
		"BASIC_USER": "ignored", "BASIC_PASSWORD": "short", "ALLOWED_HOSTS": "ignored",
		"ALLOWED_ORIGINS": "ignored", "MCP_ALLOW_DESTRUCTIVE": "true", "STDIO_READ_ONLY": "true",
	} {
		t.Setenv("WORKLOG_"+key, value)
	}
}

func TestDefaultsAndTokenPermissions(t *testing.T) {
	t.Chdir(t.TempDir())
	poisonBusinessEnvironment(t)
	// Server startup must not implicitly discover config.json.
	if err := os.WriteFile("config.json", []byte(`{"addr":"bad-address"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.Addr != "127.0.0.1:12450" || !c.Local || c.Driver != "sqlite" || c.DSN != "./data/worklog.db" || c.User != "owner" || c.Timezone != "Asia/Shanghai" || len(c.WriteToken) != 64 || c.ReadToken != "" || c.MCPDestructive || c.StdioReadOnly || c.BasicUser != "worklog" || c.BasicPassword != "" || c.StaticDir != "Web/dist" || c.TokenFile != "./data/mcp-token" || strings.Join(c.AllowedHosts, ",") != "localhost,127.0.0.1,::1" || strings.Join(c.AllowedOrigins, ",") != "http://localhost:5173,http://127.0.0.1:5173" {
		t.Fatal("incorrect defaults")
	}
	second, e := Load()
	if e != nil || second.WriteToken != c.WriteToken {
		t.Fatal("token not persistent", e)
	}
	if e = makeTokenInsecure(c.TokenFile); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(); e == nil {
		t.Fatal("insecure token file accepted")
	}
}

func TestBasicPasswordLength(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:12450", "0.0.0.0:12450"} {
		for _, length := range []int{0, 7, 8} {
			t.Run(addr+"/"+strings.Repeat("p", length), func(t *testing.T) {
				path := writeConfig(t, map[string]any{
					"addr": addr, "write_token": strings.Repeat("w", 32),
					"basic_password": strings.Repeat("p", length),
				})
				_, err := LoadFile(path)
				wantError := length == 7 || (length == 0 && addr == "0.0.0.0:12450")
				if (err != nil) != wantError {
					t.Fatalf("length=%d: error=%v, wantError=%v", length, err, wantError)
				}
			})
		}
	}
}

func TestConfigurationValidationUsesJSONFieldNames(t *testing.T) {
	for _, tc := range []struct {
		fields map[string]any
		field  string
	}{
		{map[string]any{"addr": "invalid"}, "addr"},
		{map[string]any{"addr": "0.0.0.0:12450"}, "write_token"},
		{map[string]any{"timezone": "invalid/place"}, "timezone"},
		{map[string]any{"user_id": ""}, "user_id"},
		{map[string]any{"db_driver": "invalid"}, "db_driver"},
		{map[string]any{"db_driver": "postgres"}, "db_dsn"},
		{map[string]any{"db_driver": "postgres", "db_dsn": ""}, "db_dsn"},
		{map[string]any{"db_driver": "postgres", "db_dsn": " \t\n"}, "db_dsn"},
		{map[string]any{"basic_password": "short"}, "basic_password"},
		{map[string]any{"read_token": strings.Repeat("w", 32)}, "read_token"},
		{map[string]any{"write_token": "short"}, "write_token"},
		{map[string]any{"static_dir": "."}, "static_dir"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			if _, ok := tc.fields["write_token"]; !ok && tc.field != "write_token" {
				tc.fields["write_token"] = strings.Repeat("w", 32)
			}
			_, err := LoadFile(writeConfig(t, tc.fields))
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected error naming %s: %v", tc.field, err)
			}
		})
	}
}

func TestRemoteConfiguration(t *testing.T) {
	fields := map[string]any{
		"addr": "0.0.0.0:12450", "write_token": strings.Repeat("w", 32),
		"basic_password": strings.Repeat("h", 8), "read_token": strings.Repeat("w", 32),
	}
	if _, err := LoadFile(writeConfig(t, fields)); err == nil {
		t.Fatal("equal tokens accepted")
	}
	fields["read_token"] = strings.Repeat("r", 32)
	if _, err := LoadFile(writeConfig(t, fields)); err != nil {
		t.Fatal(err)
	}
}
