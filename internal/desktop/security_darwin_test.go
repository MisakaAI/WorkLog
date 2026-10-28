//go:build darwin && cgo

package desktop

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func TestMacPrivateDataDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Worklog 数据 with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "private")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataDir(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataDir(root); err == nil {
		t.Fatal("accepted shared file")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0644 {
		t.Fatal("silently changed existing permissions")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataRoot(root); err == nil {
		t.Fatal("accepted shared root")
	}
	if err := SecureDataDir(path); err == nil {
		t.Fatal("accepted non-directory root")
	}
	if err := SecureDataDir(""); err == nil {
		t.Fatal("accepted empty root")
	}
}

func TestMacDataDirectoryRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataRoot(root); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataRoot(link); err == nil {
		t.Fatal("accepted symlink root")
	}
	if err := SecureDataDir(root); err == nil {
		t.Fatal("accepted symlink child")
	}
}

func TestMacDataDirectoryRejectsExtendedACL(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataRoot(root); err != nil {
		t.Fatal(err)
	}
	// Even an owner-only extra ACL fails closed; we do not attempt to infer
	// effective access, and mode bits alone cannot detect this ACL.
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("/bin/chmod", "+a", "user:"+current.Username+" allow readattr", root).CombinedOutput()
	if err != nil {
		t.Fatalf("create test ACL: %v\n%s", err, output)
	}
	if err := SecureDataRoot(root); err == nil {
		t.Fatal("accepted extended ACL")
	}
}

func TestMacOpenBrowserRejectsInvalidURL(t *testing.T) {
	for _, address := range []string{"", "relative", "file:///tmp/worklog", "javascript:alert(1)", "http:///missing-host", "https://example.com/\x00", "https://example.com/\n"} {
		if err := OpenBrowser(address); err == nil {
			t.Errorf("accepted URL %q", address)
		}
	}
}
