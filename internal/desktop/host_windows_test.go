//go:build windows

package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeAMD64Layout(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("Windows amd64 ABI assertions")
	}
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"POINT size", unsafe.Sizeof(nativePoint{}), 8},
		{"MSG size", unsafe.Sizeof(nativeMessage{}), 48},
		{"MSG.wParam", unsafe.Offsetof(nativeMessage{}.WParam), 16},
		{"MSG.lParam", unsafe.Offsetof(nativeMessage{}.LParam), 24},
		{"MSG.pt", unsafe.Offsetof(nativeMessage{}.Point), 36},
		{"WNDCLASSEXW size", unsafe.Sizeof(nativeWindowClass{}), 80},
		{"WNDCLASSEXW.lpfnWndProc", unsafe.Offsetof(nativeWindowClass{}.WndProc), 8},
		{"WNDCLASSEXW.hInstance", unsafe.Offsetof(nativeWindowClass{}.Instance), 24},
		{"WNDCLASSEXW.lpszClassName", unsafe.Offsetof(nativeWindowClass{}.ClassName), 64},
		{"NOTIFYICONDATAW size", unsafe.Sizeof(nativeNotifyIcon{}), 976},
		{"NOTIFYICONDATAW.hWnd", unsafe.Offsetof(nativeNotifyIcon{}.Window), 8},
		{"NOTIFYICONDATAW.hIcon", unsafe.Offsetof(nativeNotifyIcon{}.Icon), 32},
		{"NOTIFYICONDATAW.szTip", unsafe.Offsetof(nativeNotifyIcon{}.Tip), 40},
		{"NOTIFYICONDATAW.szInfo", unsafe.Offsetof(nativeNotifyIcon{}.Info), 304},
		{"NOTIFYICONDATAW.uVersion", unsafe.Offsetof(nativeNotifyIcon{}.TimeoutVersion), 816},
		{"NOTIFYICONDATAW.guidItem", unsafe.Offsetof(nativeNotifyIcon{}.GUID), 952},
		{"NOTIFYICONDATAW.hBalloonIcon", unsafe.Offsetof(nativeNotifyIcon{}.BalloonIcon), 968},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}

func instanceTestName(t *testing.T) string {
	return fmt.Sprintf(`Local\Worklog.Test.%d.%d.%s`, os.Getpid(), time.Now().UnixNano(), t.Name())
}

func TestInstanceNotificationBeforeWatch(t *testing.T) {
	name := instanceTestName(t)
	primary, isPrimary, err := acquireNamedInstance(name)
	if err != nil || !isPrimary || primary == nil {
		t.Fatalf("primary election: instance=%v primary=%v error=%v", primary, isPrimary, err)
	}
	defer primary.Close()
	secondary, isPrimary, err := acquireNamedInstance(name)
	if err != nil || isPrimary || secondary != nil {
		t.Fatalf("secondary notification: instance=%v primary=%v error=%v", secondary, isPrimary, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		primary.Watch(ctx, func() { opened <- struct{}{} })
	}()
	select {
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("notification sent during startup was lost")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not stop after cancellation")
	}
}

func TestConcurrentInstanceElection(t *testing.T) {
	name := instanceTestName(t)
	type result struct {
		instance *Instance
		primary  bool
		err      error
	}
	const count = 32
	start := make(chan struct{})
	results := make(chan result, count)
	for n := 0; n < count; n++ {
		go func() {
			<-start
			i, primary, err := acquireNamedInstance(name)
			results <- result{i, primary, err}
		}()
	}
	close(start)
	var primary *Instance
	primaries := 0
	for n := 0; n < count; n++ {
		r := <-results
		if r.err != nil {
			t.Errorf("contender error: %v", r.err)
		}
		if r.primary {
			primaries++
			primary = r.instance
			defer r.instance.Close()
		}
	}
	if primaries != 1 {
		t.Fatalf("elected %d primaries, want one", primaries)
	}
	// At least one secondary notification must survive until Watch starts.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		primary.Watch(ctx, func() {
			select {
			case opened <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent launch notification lost")
	}
	if err := primary.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not stop after Close")
	}
	if err := primary.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	replacement, isPrimary, err := acquireNamedInstance(name)
	if replacement != nil {
		defer replacement.Close()
	}
	if err != nil || !isPrimary || replacement == nil {
		t.Fatalf("marker survived Close: primary=%v error=%v", isPrimary, err)
	}
}

func TestInstanceCloseReleasesOwnershipWithLingeringHandle(t *testing.T) {
	name := instanceTestName(t)
	primary, isPrimary, err := acquireNamedInstance(name)
	if err != nil || !isPrimary || primary == nil {
		t.Fatalf("primary election: primary=%v error=%v", isPrimary, err)
	}
	defer primary.Close()
	mutexName, _ := windows.UTF16PtrFromString(name + ".Instance")
	lingering, err := windows.CreateMutex(nil, false, mutexName)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		t.Fatal(err)
	}
	defer windows.CloseHandle(lingering)

	// All concurrent callers must wait for the owner's native cleanup, not
	// merely for the first Close caller to mark the instance closed.
	const closers = 8
	results := make(chan error, closers)
	for n := 0; n < closers; n++ {
		go func() {
			err := primary.Close()
			select {
			case <-primary.ownerDone:
			default:
				err = fmt.Errorf("Close returned before owner cleanup: %v", err)
			}
			results <- err
		}()
	}
	for n := 0; n < closers; n++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	replacement, isPrimary, err := acquireNamedInstance(name)
	if replacement != nil {
		defer replacement.Close()
	}
	if err != nil || !isPrimary || replacement == nil {
		t.Fatalf("lingering handle prevented election: primary=%v error=%v", isPrimary, err)
	}
}

func TestInstanceRecoversAbandonedMutex(t *testing.T) {
	name := instanceTestName(t)
	mutexName, _ := windows.UTF16PtrFromString(name + ".Instance")
	// Keep the object alive after the subprocess dies, reproducing a handle
	// retained by a suspended secondary. Existence alone must not elect it.
	lingering, err := windows.CreateMutex(nil, false, mutexName)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(lingering)
	child := exec.Command(os.Args[0], "-test.run=^TestInstanceAbandonmentHelper$")
	child.Env = append(os.Environ(), "WORKLOG_TEST_ABANDON_INSTANCE="+name)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("abandonment helper: %v\n%s", err, output)
	}
	replacement, isPrimary, err := acquireNamedInstance(name)
	if replacement != nil {
		defer replacement.Close()
	}
	if err != nil || !isPrimary || replacement == nil {
		t.Fatalf("abandoned mutex prevented election: primary=%v error=%v", isPrimary, err)
	}
}

func TestInstanceAbandonmentHelper(t *testing.T) {
	name := os.Getenv("WORKLOG_TEST_ABANDON_INSTANCE")
	if name == "" {
		return
	}
	instance, isPrimary, err := acquireNamedInstance(name)
	if err != nil || !isPrimary || instance == nil {
		t.Fatalf("helper primary election: primary=%v error=%v", isPrimary, err)
	}
	// Deliberately bypass Close and all defers. The OS abandons the mutex
	// owned by the dedicated thread when the entire subprocess terminates.
	os.Exit(0)
}

func TestFillUTF16(t *testing.T) {
	for _, test := range []struct {
		text string
		size int
		want string
	}{
		{"anything", 0, ""},
		{"abc", 1, ""},
		{"abc", 3, "ab"},
		{"😀x", 2, ""},
		{"😀x", 3, "😀"},
		{"a😀", 3, "a"},
		{"运行中", 8, "运行中"},
		{"a\x00b", 8, "a\uFFFDb"},
	} {
		buffer := make([]uint16, test.size)
		for n := range buffer {
			buffer[n] = 0xffff
		}
		fillUTF16(buffer, test.text)
		end := 0
		for end < len(buffer) && buffer[end] != 0 {
			end++
		}
		if end == len(buffer) && len(buffer) != 0 {
			t.Errorf("%q: missing terminator", test.text)
		}
		if got := string(utf16.Decode(buffer[:end])); got != test.want {
			t.Errorf("fillUTF16(%q, %d) = %q, want %q", test.text, test.size, got, test.want)
		}
	}
}

func TestMenuText(t *testing.T) {
	if got := windows.UTF16PtrToString(menuText("服务 A&B\x00")); got != "服务 A&&B\uFFFD" {
		t.Fatalf("menu label = %q", got)
	}
}

func TestOpenBrowserRejectsNonWebURLs(t *testing.T) {
	for _, address := range []string{"", "relative", "file:///C:/Windows/notepad.exe", "javascript:alert(1)", "http:///missing-host", "https://example.com/\x00"} {
		if err := OpenBrowser(address); err == nil {
			t.Errorf("accepted non-web URL %q", address)
		}
	}
}

func TestSecureDataDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WorklogDesktop")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataDir(path); err != nil {
		t.Fatal(err)
	}
	if err := SecureDataDir(path); err != nil {
		t.Fatalf("securing twice: %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	assertRestrictedDACL(t, path, user.User.Sid.String(), true)
	child := filepath.Join(path, "secret.txt")
	if err := os.WriteFile(child, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	assertRestrictedDACL(t, child, user.User.Sid.String(), false)
	if err := SecureDataDir(child); err == nil {
		t.Fatal("accepted a regular file as the data directory")
	}
	if err := SecureDataDir(""); err == nil {
		t.Fatal("accepted an empty data directory path")
	}
}

func assertRestrictedDACL(t *testing.T, path, userSID string, protected bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		t.Fatalf("read DACL: descriptor=%v error=%v", sd, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if protected && control&windows.SE_DACL_PROTECTED == 0 {
		t.Error("directory DACL is not protected against parent inheritance")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("missing DACL: %v", err)
	}
	if dacl.AceCount != 2 {
		t.Fatalf("DACL contains %d ACEs instead of exactly user + SYSTEM: %s", dacl.AceCount, sd.String())
	}
	seen := make(map[string]bool)
	for n := uint32(0); n < uint32(dacl.AceCount); n++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, n, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		seen[sid] = true
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != 0x1f01ff { // FILE_ALL_ACCESS
			t.Errorf("unexpected access grant: %s", sd.String())
		}
		if protected && ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
			t.Error("directory ACE is not inheritable by both files and directories")
		}
	}
	if !seen[userSID] || !seen["S-1-5-18"] {
		t.Errorf("unexpected trustees: %v", seen)
	}
	runtime.KeepAlive(sd)
}
