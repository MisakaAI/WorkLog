package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func setTokenTestDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}

func makeTokenInsecure(path string) error {
	return setTokenTestDACL(path, "D:P(A;;FA;;;WD)")
}

func TestWindowsTokenSecurityDescriptor(t *testing.T) {
	user, err := tokenUserSID()
	if err != nil {
		t.Fatal(err)
	}
	owner := "O:" + user.String()
	userACE := "(A;;FA;;;" + user.String() + ")"
	for _, tt := range []struct {
		name      string
		sddl      string
		wantError bool
	}{
		{"user only", owner + "D:P" + userACE, false},
		{"desktop user and system", owner + "D:P" + userACE + "(A;;FA;;;SY)", false},
		{"user system admins", owner + "D:P" + userACE + "(A;;FA;;;SY)(A;;FA;;;BA)", false},
		{"inherited private", owner + "D:(A;ID;FA;;;" + user.String() + ")(A;ID;FA;;;SY)", false},
		{"system owner", "O:SYD:P" + userACE, false},
		{"admin owner", "O:BAD:P" + userACE, false},
		{"empty DACL", owner + "D:P", false},
		{"broad deny", owner + "D:P(D;;FA;;;WD)" + userACE, false},
		{"inherit only broad allow", owner + "D:P" + userACE + "(A;OIIO;FA;;;WD)", false},
		{"everyone", owner + "D:P" + userACE + "(A;;FR;;;WD)", true},
		{"builtin users", owner + "D:P" + userACE + "(A;;FR;;;BU)", true},
		{"authenticated users", owner + "D:P" + userACE + "(A;;FR;;;AU)", true},
		{"other user", owner + "D:P" + userACE + "(A;;FA;;;S-1-5-21-1-2-3-1001)", true},
		{"inherited everyone", owner + "D:P" + userACE + "(A;ID;FR;;;WD)", true},
		{"broad write only", owner + "D:P" + userACE + "(A;;FW;;;WD)", true},
		{"broad metadata only", owner + "D:P" + userACE + "(A;;RC;;;WD)", true},
		{"deny does not excuse broad allow", owner + "D:P(D;;FA;;;WD)" + userACE + "(A;;FR;;;WD)", true},
		{"object allow fails closed", owner + "D:P" + userACE + "(OA;;FR;00112233-4455-6677-8899-aabbccddeeff;;WD)", true},
		{"untrusted owner", "O:S-1-5-21-1-2-3-1001D:P" + userACE, true},
		{"missing owner", "D:P" + userACE, true},
		{"missing DACL", owner, true},
		{"NULL DACL", owner + "D:NO_ACCESS_CONTROL", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tt.sddl)
			if err != nil {
				t.Fatalf("invalid test descriptor %q: %v", tt.sddl, err)
			}
			err = validateTokenSecurityDescriptor(sd, user)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tt.wantError)
			}
		})
	}
	if err := validateTokenSecurityDescriptor(nil, user); err == nil {
		t.Fatal("nil security descriptor accepted")
	}
}

func TestWindowsTokenPrivateInPermissiveDirectory(t *testing.T) {
	dir := t.TempDir()
	// Model server use outside the desktop's protected APPDATA directory.
	if err := setTokenTestDACL(dir, "D:P(A;OICI;FA;;;WD)"); err != nil {
		t.Fatal(err)
	}
	file, err := createPrivateTokenTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || !info.Mode().IsRegular() {
		t.Fatalf("new token temp not empty and regular: %v", info)
	}
	if err := checkTokenPermissions(file, info); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("new token DACL is not protected: control=%x err=%v", control, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 3 {
		t.Fatalf("expected exactly user/SYSTEM/Administrators ACEs: dacl=%v err=%v", dacl, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "token")
	first, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateToken(path)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("private token did not reload: equal=%v err=%v", first == second, err)
	}
	assertNoTokenTemps(t, dir)
}

func TestWindowsRejectedTokenACLUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	contents := strings.Repeat("x", 64)
	if err := publishToken(path, contents); err != nil {
		t.Fatal(err)
	}
	if err := makeTokenInsecure(path); err != nil {
		t.Fatal(err)
	}
	getSD := func() string {
		t.Helper()
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		return sd.String()
	}
	before := getSD()
	if _, err := loadOrCreateToken(path); err == nil {
		t.Fatal("broad DACL accepted")
	}
	if after := getSD(); after != before {
		t.Fatalf("existing ACL changed: before=%q after=%q", before, after)
	}
	assertTokenContents(t, path, contents)
}
