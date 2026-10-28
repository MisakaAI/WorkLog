//go:build windows && desktop

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateConfigTempACL(t *testing.T) {
	file, err := createPrivateConfigTemp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPrivateConfigACL(sd, sd, user.User.Sid); err != nil {
		t.Fatal(err)
	}
	broad, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPrivateConfigACL(broad, sd, user.User.Sid); err == nil {
		t.Fatal("public ACL accepted")
	}
}
