//go:build windows

package desktop

import (
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestAutostartCommand(t *testing.T) {
	for _, tt := range []struct {
		name, executable, conf, want string
	}{
		{
			name:       "always quote executable",
			executable: `C:\Worklog\worklog.exe`,
			want:       `"C:\Worklog\worklog.exe" --autostart`,
		},
		{
			name:       "spaces",
			executable: `C:\Program Files\Worklog\worklog.exe`,
			conf:       `C:\User Data\Worklog\config.yaml`,
			want:       `"C:\Program Files\Worklog\worklog.exe" --autostart --conf "C:\User Data\Worklog\config.yaml"`,
		},
		{
			name:       "Unicode",
			executable: `C:\工具\日志😀.exe`,
			conf:       `C:\用户\配置😀.yaml`,
			want:       `"C:\工具\日志😀.exe" --autostart --conf C:\用户\配置😀.yaml`,
		},
		{
			name:       "config trailing slash with spaces",
			executable: `C:\Worklog\worklog.exe`,
			conf:       `C:\User Data\Worklog\`,
			want:       `"C:\Worklog\worklog.exe" --autostart --conf "C:\User Data\Worklog\\"`,
		},
		{
			name:       "config trailing slash without spaces",
			executable: `C:\Worklog\worklog.exe`,
			conf:       `C:\Worklog\`,
			want:       `"C:\Worklog\worklog.exe" --autostart --conf C:\Worklog\`,
		},
		{
			name:       "UNC paths",
			executable: `\\server\shared apps\worklog.exe`,
			conf:       `\\server\config\worklog.yaml`,
			want:       `"\\server\shared apps\worklog.exe" --autostart --conf \\server\config\worklog.yaml`,
		},
		{
			name:       "no environment expansion or cleaning",
			executable: `C:\%WORKLOG_HOME%\worklog.exe`,
			conf:       `C:\~\config\..\%WORKLOG_CONFIG%.yaml`,
			want:       `"C:\%WORKLOG_HOME%\worklog.exe" --autostart --conf C:\~\config\..\%WORKLOG_CONFIG%.yaml`,
		},
		{
			name:       "escape config quotes",
			executable: `C:\Worklog\worklog.exe`,
			conf:       `C:\User Data\a"b.yaml`,
			want:       `"C:\Worklog\worklog.exe" --autostart --conf "C:\User Data\a\"b.yaml"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAutostart(tt.executable, tt.conf)
			if err != nil {
				t.Fatal(err)
			}
			if a.command != tt.want {
				t.Fatalf("command = %q, want %q", a.command, tt.want)
			}
			if a.keyPath != `Software\Microsoft\Windows\CurrentVersion\Run` || a.valueName != "WorklogDesktop" {
				t.Fatalf("unexpected registry location: %q / %q", a.keyPath, a.valueName)
			}
			args, err := windows.DecomposeCommandLine(a.command)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := []string{tt.executable, "--autostart"}
			if tt.conf != "" {
				wantArgs = append(wantArgs, "--conf", tt.conf)
			}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("parsed arguments = %q, want %q", args, wantArgs)
			}
		})
	}
}

func TestAutostartRejectsInvalidPaths(t *testing.T) {
	const exe = `C:\Worklog\worklog.exe`
	for _, tt := range []struct {
		name, executable, conf, field string
	}{
		{"empty executable", "", "", "executable"},
		{"relative executable", `worklog.exe`, "", "executable"},
		{"drive relative executable", `C:worklog.exe`, "", "executable"},
		{"root relative executable", `\worklog.exe`, "", "executable"},
		{"executable environment variable", `%USERPROFILE%\worklog.exe`, "", "executable"},
		{"executable tilde", `~\worklog.exe`, "", "executable"},
		{"executable NUL", exe + "\x00other", "", "executable"},
		{"relative config", exe, `config.yaml`, "configuration"},
		{"drive relative config", exe, `C:config.yaml`, "configuration"},
		{"root relative config", exe, `\config.yaml`, "configuration"},
		{"config environment variable", exe, `%APPDATA%\config.yaml`, "configuration"},
		{"config tilde", exe, `~\config.yaml`, "configuration"},
		{"config NUL", exe, "C:\\config.yaml\x00other", "configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAutostart(tt.executable, tt.conf)
			if a != nil || err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("NewAutostart = %v, %v; want nil and %s error", a, err, tt.field)
			}
		})
	}
}

// Every test doing registry I/O redirects the helper before using it. A random
// top-level Software child needs no shared test parent and never touches Run.
func newRegistryTestAutostart(t *testing.T) *Autostart {
	t.Helper()
	a, err := NewAutostart(`C:\Worklog\日志😀.exe`, `C:\User Data\%CONFIG%\config.yaml`)
	if err != nil {
		t.Fatal(err)
	}
	a.keyPath = `Software\WorklogAutostartTest-` + rand.Text()
	t.Cleanup(func() {
		if err := registry.DeleteKey(registry.CURRENT_USER, a.keyPath); err != nil && !autostartNotExist(err) {
			t.Errorf("delete test registry key: %v", err)
		}
	})
	return a
}

func assertAutostartEnabled(t *testing.T, a *Autostart, want bool) {
	t.Helper()
	got, err := a.Enabled()
	if err != nil || got != want {
		t.Fatalf("Enabled = %v, %v; want %v, nil", got, err, want)
	}
}

func TestAutostartMissingKeyDoesNotCreate(t *testing.T) {
	a := newRegistryTestAutostart(t)
	for attempt := 0; attempt < 2; attempt++ {
		assertAutostartEnabled(t, a, false)
		if err := a.SetEnabled(false); err != nil {
			t.Fatal(err)
		}
		key, err := registry.OpenKey(registry.CURRENT_USER, a.keyPath, registry.QUERY_VALUE)
		if err == nil {
			key.Close()
			t.Fatal("querying or disabling autostart created a key")
		}
		if !autostartNotExist(err) {
			t.Fatal(err)
		}
	}
	// Enabling, in contrast, creates the missing key and value.
	if err := a.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	assertAutostartEnabled(t, a, true)
}

func TestAutostartEnableDisableIdempotent(t *testing.T) {
	a := newRegistryTestAutostart(t)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, a.keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetStringValue("OtherApplication", "leave this alone"); err != nil {
		t.Fatal(err)
	}
	assertAutostartEnabled(t, a, false) // Existing key, missing value.
	if err := a.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := a.SetEnabled(true); err != nil {
			t.Fatal(err)
		}
		assertAutostartEnabled(t, a, true)
		command, typ, err := key.GetStringValue(a.valueName)
		if err != nil || typ != registry.SZ || command != a.command {
			t.Fatalf("stored command = %q, type %d, %v; want raw REG_SZ %q", command, typ, err, a.command)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := a.SetEnabled(false); err != nil {
			t.Fatal(err)
		}
		assertAutostartEnabled(t, a, false)
		if _, _, err := key.GetStringValue(a.valueName); !errors.Is(err, registry.ErrNotExist) {
			t.Fatalf("disabled value still present: %v", err)
		}
		// The key and unrelated values must survive disabling.
		other, typ, err := key.GetStringValue("OtherApplication")
		if err != nil || typ != registry.SZ || other != "leave this alone" {
			t.Fatalf("unrelated value changed: %q, type %d, %v", other, typ, err)
		}
	}
}

func TestAutostartMismatchedCommand(t *testing.T) {
	a := newRegistryTestAutostart(t)
	if err := a.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, exe, conf string }{
		{"moved executable", `D:\Worklog\worklog.exe`, `C:\User Data\%CONFIG%\config.yaml`},
		{"changed config", `C:\Worklog\日志😀.exe`, `C:\Worklog\other.yaml`},
		{"removed config", `C:\Worklog\日志😀.exe`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed, err := NewAutostart(tt.exe, tt.conf)
			if err != nil {
				t.Fatal(err)
			}
			changed.keyPath, changed.valueName = a.keyPath, a.valueName
			assertAutostartEnabled(t, changed, false)
			assertAutostartEnabled(t, a, true) // A read must not repair the entry.
		})
	}
}

func TestAutostartUnexpectedValues(t *testing.T) {
	a := newRegistryTestAutostart(t)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, a.keyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	for _, tt := range []struct {
		name    string
		write   func() error
		typeErr bool
	}{
		{"empty string", func() error { return key.SetStringValue(a.valueName, "") }, false},
		{"unrelated command", func() error { return key.SetStringValue(a.valueName, `"C:\other.exe"`) }, false},
		{"extra argument", func() error { return key.SetStringValue(a.valueName, a.command+" --extra") }, false},
		{"DWORD", func() error { return key.SetDWordValue(a.valueName, 1) }, true},
		{"binary", func() error { return key.SetBinaryValue(a.valueName, []byte{0xff}) }, true},
		{"empty binary", func() error { return key.SetBinaryValue(a.valueName, nil) }, true},
		{"multi string", func() error { return key.SetStringsValue(a.valueName, []string{a.command}) }, true},
		{"expand string", func() error { return key.SetExpandStringValue(a.valueName, a.command) }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.write(); err != nil {
				t.Fatal(err)
			}
			got, err := a.Enabled()
			if got || (err != nil) != tt.typeErr {
				t.Fatalf("Enabled = %v, %v; want false, type error = %v", got, err, tt.typeErr)
			}
			if tt.typeErr && (!errors.Is(err, registry.ErrUnexpectedType) || !strings.Contains(err.Error(), "REG_SZ")) {
				t.Fatalf("unhelpful type error: %v", err)
			}
			// Enabling repairs any stale command or unexpected registry type.
			if err := a.SetEnabled(true); err != nil {
				t.Fatal(err)
			}
			assertAutostartEnabled(t, a, true)
			if err := tt.write(); err != nil {
				t.Fatal(err)
			}
			if err := a.SetEnabled(false); err != nil {
				t.Fatal(err)
			}
			assertAutostartEnabled(t, a, false)
		})
	}
}
