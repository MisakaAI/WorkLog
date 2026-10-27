package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultsAndTokenPermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKLOG_TOKEN_FILE", filepath.Join(dir, "private", "token"))
	t.Setenv("WORKLOG_ADDR", "127.0.0.1:12450")
	t.Setenv("WORKLOG_WRITE_TOKEN", "")
	t.Setenv("WORKLOG_READ_TOKEN", "")
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if !c.Local || c.Driver != "sqlite" || c.Timezone != "Asia/Shanghai" || len(c.WriteToken) != 64 || c.MCPDestructive {
		t.Fatal(c)
	}
	second, e := Load()
	if e != nil || second.WriteToken != c.WriteToken {
		t.Fatal("token not persistent", e)
	}
	if runtime.GOOS != "windows" {
		if e = os.Chmod(filepath.Join(dir, "private", "token"), 0644); e != nil {
			t.Fatal(e)
		}
		if _, e = Load(); e == nil {
			t.Fatal("insecure token file accepted")
		}
	}
}
func TestBasicPasswordLength(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:12450", "0.0.0.0:12450"} {
		for _, length := range []int{0, 7, 8} {
			t.Run(addr+"/"+strings.Repeat("p", length), func(t *testing.T) {
				t.Setenv("WORKLOG_ADDR", addr)
				t.Setenv("WORKLOG_WRITE_TOKEN", strings.Repeat("w", 32))
				t.Setenv("WORKLOG_READ_TOKEN", "")
				t.Setenv("WORKLOG_BASIC_PASSWORD", strings.Repeat("p", length))
				_, err := Load()
				wantError := length == 7 || (length == 0 && addr == "0.0.0.0:12450")
				if (err != nil) != wantError {
					t.Fatalf("length=%d: error=%v, wantError=%v", length, err, wantError)
				}
			})
		}
	}
}

func TestRemoteConfiguration(t *testing.T) {
	t.Setenv("WORKLOG_ADDR", "0.0.0.0:12450")
	t.Setenv("WORKLOG_WRITE_TOKEN", "")
	t.Setenv("WORKLOG_BASIC_PASSWORD", "")
	if _, e := Load(); e == nil {
		t.Fatal("unprotected remote bind")
	}
	t.Setenv("WORKLOG_WRITE_TOKEN", strings.Repeat("w", 32))
	t.Setenv("WORKLOG_BASIC_PASSWORD", strings.Repeat("h", 8))
	t.Setenv("WORKLOG_READ_TOKEN", strings.Repeat("w", 32))
	if _, e := Load(); e == nil {
		t.Fatal("equal tokens")
	}
	t.Setenv("WORKLOG_READ_TOKEN", strings.Repeat("r", 32))
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("WORKLOG_TIMEZONE", "invalid/place")
	if _, e := Load(); e == nil {
		t.Fatal("bad timezone")
	}
}
