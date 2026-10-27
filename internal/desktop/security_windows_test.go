//go:build windows

package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func dataSecurityTestUser(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid
}

func TestDataSecurityDescriptor(t *testing.T) {
	user := dataSecurityTestUser(t)
	owner := "O:" + user.String()
	userACE := "(A;;FA;;;" + user.String() + ")"
	for _, tt := range []struct {
		name string
		sddl string
		bad  bool
	}{
		{"user", owner + "D:P" + userACE, false},
		{"user and system", owner + "D:P" + userACE + "(A;;FA;;;SY)", false},
		{"user system admins", owner + "D:P" + userACE + "(A;;FA;;;SY)(A;;FA;;;BA)", false},
		{"inherited private", owner + "D:(A;ID;FA;;;" + user.String() + ")(A;ID;FA;;;SY)", false},
		{"system owner", "O:SYD:P" + userACE, false},
		{"admin owner", "O:BAD:P" + userACE, false},
		{"empty DACL", owner + "D:P", false},
		{"broad deny", owner + "D:P(D;;FA;;;WD)" + userACE, false},
		{"private inherit only", owner + "D:P(A;OICIIO;FA;;;" + user.String() + ")", false},
		{"everyone", owner + "D:P" + userACE + "(A;;FR;;;WD)", true},
		{"users", owner + "D:P" + userACE + "(A;;FR;;;BU)", true},
		{"authenticated users", owner + "D:P" + userACE + "(A;;FR;;;AU)", true},
		{"other user", owner + "D:P" + userACE + "(A;;FA;;;S-1-5-21-1-2-3-1001)", true},
		{"inherited broad", owner + "D:P" + userACE + "(A;ID;FR;;;WD)", true},
		{"inherit only broad", owner + "D:P" + userACE + "(A;OICIIO;FA;;;WD)", true},
		{"write only broad", owner + "D:P" + userACE + "(A;;FW;;;WD)", true},
		{"metadata only broad", owner + "D:P" + userACE + "(A;;RC;;;WD)", true},
		{"deny does not excuse allow", owner + "D:P(D;;FA;;;WD)" + userACE + "(A;;FR;;;WD)", true},
		{"object allow unsupported", owner + "D:P" + userACE + "(OA;;FR;00112233-4455-6677-8899-aabbccddeeff;;WD)", true},
		{"object deny unsupported", owner + "D:P(OD;;FR;00112233-4455-6677-8899-aabbccddeeff;;WD)" + userACE, true},
		{"untrusted owner", "O:S-1-5-21-1-2-3-1001D:P" + userACE, true},
		{"missing owner", "D:P" + userACE, true},
		{"missing DACL", owner, true},
		{"NULL DACL", owner + "D:NO_ACCESS_CONTROL", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tt.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateDataSecurityDescriptor(sd, user); (err != nil) != tt.bad {
				t.Fatalf("validation error = %v, want rejection = %v", err, tt.bad)
			}
		})
	}
	if err := validateDataSecurityDescriptor(nil, user); err == nil {
		t.Fatal("accepted nil descriptor")
	}
	sd, err := windows.SecurityDescriptorFromString(owner + "D:P" + userACE)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDataSecurityDescriptor(sd, nil); err == nil {
		t.Fatal("accepted nil user")
	}
}

func setDataSecurityTestDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	runtime.KeepAlive(sd)
	if err != nil {
		t.Fatal(err)
	}
}

func dataSecurityTestDescriptor(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func TestSecureDataDirRejectsExistingProtectedBroadEntries(t *testing.T) {
	user := dataSecurityTestUser(t)
	const contents = "test-secret-content-must-not-appear-in-errors"
	for _, tt := range []struct {
		name string
		dir  bool
	}{
		{"logs", true},
		{"logs/desktop.log", false},
		{"config.yaml", false},
		{"worklog.db", false},
		{"worklog.db-wal", false},
		{"worklog.db-shm", false},
		{"data/nested/worklog.db", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "app")
			path := filepath.Join(root, filepath.FromSlash(tt.name))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if tt.dir {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			// A protected child does not inherit the root's replacement DACL.
			setDataSecurityTestDACL(t, path, "D:P(A;;FA;;;"+user.String()+")(A;;FR;;;WD)")
			before := dataSecurityTestDescriptor(t, path)
			for attempt := 0; attempt < 2; attempt++ {
				err := SecureDataDir(root)
				if err == nil {
					t.Fatal("accepted existing protected broad entry")
				}
				if strings.Contains(err.Error(), contents) {
					t.Fatal("validation error exposed file contents")
				}
				if after := dataSecurityTestDescriptor(t, path); after != before {
					t.Fatalf("child ACL was silently repaired: before=%q after=%q", before, after)
				}
			}
			if !tt.dir {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != contents {
					t.Fatalf("existing file changed: read error = %v", err)
				}
			}
		})
	}
}

func TestSecureDataDirAcceptsExistingPrivateTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataDir(root); err != nil {
		t.Fatal(err)
	}
	user := dataSecurityTestUser(t)
	for _, name := range []string{"logs/desktop.log", "config.yaml", "data/worklog.db"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Mixed inherited and explicitly protected private entries are acceptable.
	config := filepath.Join(root, "config.yaml")
	setDataSecurityTestDACL(t, config, "D:P(A;;FA;;;"+user.String()+")(A;;FA;;;SY)(A;;FR;;;BA)")
	before := dataSecurityTestDescriptor(t, config)
	if err := SecureDataDir(root); err != nil {
		t.Fatalf("rejected private existing tree: %v", err)
	}
	if after := dataSecurityTestDescriptor(t, config); after != before {
		t.Fatal("protected private child ACL changed")
	}
}

func TestSecureDataDirRejectsReparseEntries(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "app")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if directory {
				if err := os.Mkdir(outside, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(outside, []byte("outside data"), 0600); err != nil {
				t.Fatal(err)
			}
			before := dataSecurityTestDescriptor(t, outside)
			link := filepath.Join(root, "link")
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlink creation unavailable (requires Windows privilege/developer mode): %v", err)
			}
			if err := SecureDataDir(root); err == nil {
				t.Fatal("accepted reparse entry")
			}
			if after := dataSecurityTestDescriptor(t, outside); after != before {
				t.Fatal("external target ACL changed")
			}
			if directory {
				if err := SecureDataDir(link); err == nil {
					t.Fatal("accepted reparse root")
				}
			}
		})
	}
}
