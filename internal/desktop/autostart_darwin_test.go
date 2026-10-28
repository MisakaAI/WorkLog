//go:build darwin && cgo

package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testMacAutostart(t *testing.T) (*Autostart, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	bundle := filepath.Join(home, "Applications", "worklog 中文 & spaces.app")
	executable := filepath.Join(bundle, "Contents", "MacOS", "Worklog")
	if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("test fixture, never executed"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := NewAutostart(executable, filepath.Join(home, "配置 <a>&\"b\".json"))
	if err != nil {
		t.Fatal(err)
	}
	return a, bundle
}

func TestMacAutostartDoesNotRegisterOnQuery(t *testing.T) {
	a, _ := testMacAutostart(t)
	for n := 0; n < 2; n++ {
		if enabled, err := a.Enabled(); err != nil || enabled {
			t.Fatal(enabled, err)
		}
		if err := a.SetEnabled(false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Dir(a.path)); !os.IsNotExist(err) {
		t.Fatal("query/disable created LaunchAgents", err)
	}
}

func TestMacAutostartPlistAndToggle(t *testing.T) {
	a, _ := testMacAutostart(t)
	for n := 0; n < 2; n++ {
		if err := a.SetEnabled(true); err != nil {
			t.Fatal(err)
		}
		if enabled, err := a.Enabled(); err != nil || !enabled {
			t.Fatal(enabled, err)
		}
	}
	// Ask Apple's plist parser to validate XML and decode the actual arguments.
	output, err := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", a.path).CombinedOutput()
	if err != nil {
		t.Fatalf("parse LaunchAgent: %v\n%s", err, output)
	}
	var plist struct {
		Label                       string
		ProgramArguments            []string
		RunAtLoad                   bool
		KeepAlive                   bool
		LimitLoadToSessionType      string
		AssociatedBundleIdentifiers []string
	}
	if err := json.Unmarshal(output, &plist); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(os.Getenv("HOME"), "配置 <a>&\"b\".json")
	if !reflect.DeepEqual(plist.ProgramArguments, []string{a.executable, "--autostart", "--conf", conf}) {
		t.Fatal("arguments changed", plist.ProgramArguments)
	}
	if plist.Label != launchAgentLabel || !plist.RunAtLoad || plist.KeepAlive || plist.LimitLoadToSessionType != "Aqua" || !reflect.DeepEqual(plist.AssociatedBundleIdentifiers, []string{launchAgentLabel}) {
		t.Fatal("unexpected LaunchAgent settings", plist)
	}
	if info, err := os.Stat(a.path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("non-private LaunchAgent", info, err)
	}
	other := filepath.Join(filepath.Dir(a.path), "unrelated.plist")
	if err := os.WriteFile(other, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := a.SetEnabled(false); err != nil {
			t.Fatal(err)
		}
	}
	if enabled, err := a.Enabled(); err != nil || enabled {
		t.Fatal(enabled, err)
	}
	if content, err := os.ReadFile(other); err != nil || string(content) != "keep" {
		t.Fatal("changed unrelated login item", err)
	}
}

func TestMacDeletingAppDoesNotDeleteLoginPlist(t *testing.T) {
	a, bundle := testMacAutostart(t)
	if err := a.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(bundle); err != nil {
		t.Fatal(err)
	}
	if enabled, err := a.Enabled(); err != nil || !enabled {
		t.Fatal("deleting an app must not be mistaken for deleting its LaunchAgent", enabled, err)
	}
	if err := a.SetEnabled(false); err != nil {
		t.Fatal("must be able to remove stale registration", err)
	}
	if _, err := os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("registration survived disable", err)
	}
}

func TestMacAutostartDetectsChangedConfiguration(t *testing.T) {
	a, _ := testMacAutostart(t)
	if err := a.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	changed, err := NewAutostart(a.executable, filepath.Join(os.Getenv("HOME"), "other.json"))
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := changed.Enabled(); err != nil || enabled {
		t.Fatal("stale configuration reported enabled", enabled, err)
	}
	if err := changed.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if enabled, err := a.Enabled(); err != nil || enabled {
		t.Fatal("old configuration still reported enabled", enabled, err)
	}
}

func TestMacAutostartRejectsUnsafeFilesAndDirectory(t *testing.T) {
	for _, kind := range []string{"symlink-file", "symlink-directory", "shared-file", "shared-directory"} {
		t.Run(kind, func(t *testing.T) {
			a, _ := testMacAutostart(t)
			dir := filepath.Dir(a.path)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink-file":
				if err := os.Symlink(outside, a.path); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), dir); err != nil {
					t.Fatal(err)
				}
			case "shared-file":
				if err := os.WriteFile(a.path, a.contents, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(a.path, 0644); err != nil {
					t.Fatal(err)
				}
			case "shared-directory":
				if err := os.Chmod(dir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.SetEnabled(true); err == nil {
				t.Fatal("accepted unsafe login configuration")
			}
			if contents, err := os.ReadFile(outside); err != nil || !bytes.Equal(contents, []byte("keep")) {
				t.Fatal("changed symlink target", err)
			}
		})
	}
}

func TestMacAutostartRejectsInvalidPaths(t *testing.T) {
	for _, path := range []string{"", "relative", "~/Worklog", "/tmp/Worklog\x00other"} {
		if _, err := NewAutostart(path, ""); err == nil {
			t.Errorf("accepted executable %q", path)
		}
	}
	for _, path := range []string{"relative.json", "/tmp/conf\x00.json"} {
		if _, err := NewAutostart("/tmp/Worklog", path); err == nil || !strings.Contains(err.Error(), "configuration") {
			t.Errorf("accepted configuration %q: %v", path, err)
		}
	}
}
