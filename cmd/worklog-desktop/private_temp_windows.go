//go:build windows && desktop

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

// Apply the ACL at creation. Tightening it afterwards would not revoke read
// handles another user opened while the file inherited a permissive parent ACL.
func createPrivateConfigTemp(dir string) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)")
	if err != nil {
		return nil, err
	}
	defer runtime.KeepAlive(sd)
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := filepath.Abs(filepath.Join(dir, ".worklog-config-"+uuid.NewString()+".json"))
	if err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		&sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	// Fail closed on filesystems that ignore the requested security descriptor.
	actual, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err == nil {
		err = verifyPrivateConfigACL(actual, sd, user.User.Sid)
	}
	if err != nil {
		file.Close()
		os.Remove(name)
		return nil, err
	}
	return file, nil
}

func verifyPrivateConfigACL(actual, expected *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	defer runtime.KeepAlive(actual)
	defer runtime.KeepAlive(expected)
	failure := fmt.Errorf("无法确认配置临时文件的私有 Windows ACL")
	owner, _, err := actual.Owner()
	if err != nil || owner == nil || !owner.Equals(user) {
		return failure
	}
	control, _, err := actual.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return failure
	}
	a, _, err := actual.DACL()
	if err != nil || a == nil {
		return failure
	}
	b, _, err := expected.DACL()
	if err != nil || b == nil || a.AceCount != b.AceCount {
		return failure
	}
	for i := uint32(0); i < uint32(a.AceCount); i++ {
		var aa, bb *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(a, i, &aa) != nil || windows.GetAce(b, i, &bb) != nil {
			return failure
		}
		if aa.Header.AceSize != bb.Header.AceSize || !bytes.Equal(
			unsafe.Slice((*byte)(unsafe.Pointer(aa)), int(aa.Header.AceSize)),
			unsafe.Slice((*byte)(unsafe.Pointer(bb)), int(bb.Header.AceSize))) {
			return failure
		}
	}
	return nil
}
