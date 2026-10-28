//go:build windows

package desktop

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmNull            = 0x0000
	wmMeasureItem     = 0x002C
	wmDrawItem        = 0x002B
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmContextMenu     = 0x007B
	wmLButtonDblClk   = 0x0203
	wmRButtonUp       = 0x0205
	wmTrayCallback    = 0x8001 // WM_APP: never carries Go pointers
	wmTrayStatus      = 0x8002
	wmTrayClose       = 0x8003

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfOwnerDraw = 0x100
	mfDisabled  = 0x3 // MF_GRAYED | MF_DISABLED
	mfSeparator = 0x800
	mfChecked   = 0x8

	menuTitle      = 100
	menuStatus     = 101
	menuOpen       = 102
	menuRestart    = 103
	menuQuit       = 104
	menuAutostart  = 105
	menuExport     = 106
	menuImport     = 107
	iconRetryTimer = 1
)

// Native structures use pointer-sized HWND/HANDLE/WPARAM/LPARAM fields and
// natural Go alignment. In particular NOTIFYICONDATAW is 976 bytes on amd64,
// WNDCLASSEXW is 80 bytes, and MSG is 48 bytes (including lPrivate).
type nativePoint struct{ X, Y int32 }

type nativeMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   nativePoint
	Private uint32
}

type nativeWindowClass struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClassExtra int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	SmallIcon  uintptr
}

type nativeNotifyIcon struct {
	Size           uint32
	Window         uintptr
	ID             uint32
	Flags          uint32
	Callback       uint32
	Icon           uintptr
	Tip            [128]uint16
	State          uint32
	StateMask      uint32
	Info           [256]uint16
	TimeoutVersion uint32
	InfoTitle      [64]uint16
	InfoFlags      uint32
	GUID           windows.GUID
	BalloonIcon    uintptr
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	registerClassExW       = user32.NewProc("RegisterClassExW")
	unregisterClassW       = user32.NewProc("UnregisterClassW")
	createWindowExW        = user32.NewProc("CreateWindowExW")
	defWindowProcW         = user32.NewProc("DefWindowProcW")
	destroyWindow          = user32.NewProc("DestroyWindow")
	getMessageW            = user32.NewProc("GetMessageW")
	translateMessage       = user32.NewProc("TranslateMessage")
	dispatchMessageW       = user32.NewProc("DispatchMessageW")
	postMessageW           = user32.NewProc("PostMessageW")
	postQuitMessage        = user32.NewProc("PostQuitMessage")
	registerWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	loadIconW              = user32.NewProc("LoadIconW")
	createPopupMenu        = user32.NewProc("CreatePopupMenu")
	appendMenuW            = user32.NewProc("AppendMenuW")
	modifyMenuW            = user32.NewProc("ModifyMenuW")
	destroyMenu            = user32.NewProc("DestroyMenu")
	getCursorPos           = user32.NewProc("GetCursorPos")
	setForegroundWindow    = user32.NewProc("SetForegroundWindow")
	trackPopupMenu         = user32.NewProc("TrackPopupMenu")
	endMenu                = user32.NewProc("EndMenu")
	setTimer               = user32.NewProc("SetTimer")
	killTimer              = user32.NewProc("KillTimer")
	shellNotifyIconW       = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	trayWindowProc         = windows.NewCallback(trayWndProc)
	trayWindows            sync.Map // HWND -> *Tray; no Go pointer stored in Windows
	trayClassSequence      atomic.Uint64
)

// Tray is a native notification-area host. SetStatus and Close may be called
// from any goroutine; all actual UI work runs on RunTray's locked OS thread.
// Do not copy a Tray after first use.
type Tray struct {
	mu           sync.Mutex
	hwnd         uintptr // guarded by mu; zero after teardown
	closing      bool
	status       string
	statusPosted bool

	backupBusy atomic.Bool         // selection, confirmation and callback dispatch
	onBackup   func(BackupRequest) // immutable after initialization

	// The remaining fields are used exclusively by the message-loop thread.
	window         uintptr
	menu           uintptr
	menuStatusText string  // UI-thread snapshot used by owner-draw callbacks
	icon           uintptr // LoadIcon returns shared icons, never DestroyIcon
	taskbarCreated uint32
	iconAdded      bool
	retryTimer     bool
	destroyed      bool
	running        bool
	quitRequested  bool
	onOpen         func()
	onRestart      func()
	onQuit         func()
	shutdownDone   <-chan struct{}
	autostart      *Autostart
}

// RunTray runs a native Windows message loop until Tray.Close is requested.
// Lifecycle callbacks (including onReady) run on this OS thread and MUST be
// nonblocking: only enqueue application work. Quit requests call onQuit once;
// the application decides when shutdown is finished and calls Tray.Close.
// During WM_ENDSESSION only, the host waits up to four seconds for shutdownDone
// before acknowledging session termination; it never vetoes logoff/shutdown.
// The optional backup callback runs on an independent locked OS thread. Like
// the other callbacks it must only enqueue work nonblockingly; the application
// owns serialization of the actual backup operations. Only the first callback
// is used when more than one is supplied.
func RunTray(onReady func(*Tray), onOpen func(), onRestart func(), onQuit func(), shutdownDone <-chan struct{}, autostart *Autostart, onBackup ...func(BackupRequest)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var module windows.Handle
	// GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT: the executable is already
	// loaded for the process lifetime and must not be freed by this host.
	if err := windows.GetModuleHandleEx(2, nil, &module); err != nil {
		return fmt.Errorf("get executable module: %w", err)
	}
	icon, _, _ := loadIconW.Call(uintptr(module), 1) // MAKEINTRESOURCEW(1)
	if icon == 0 {
		var err error
		icon, _, err = loadIconW.Call(0, 32512) // IDI_APPLICATION, shared fallback
		if icon == 0 {
			return nativeError("load tray fallback icon", err)
		}
	}
	taskbarName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbar, _, err := registerWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarName)))
	if taskbar == 0 {
		return nativeError("register TaskbarCreated message", err)
	}

	className, _ := windows.UTF16PtrFromString(fmt.Sprintf("Worklog.Tray.%d.%d", os.Getpid(), trayClassSequence.Add(1)))
	class := nativeWindowClass{
		Size: uint32(unsafe.Sizeof(nativeWindowClass{})), WndProc: trayWindowProc,
		Instance: uintptr(module), Icon: icon, SmallIcon: icon, ClassName: className,
	}
	atom, _, err := registerClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return nativeError("register tray window class", err)
	}
	defer func() {
		if ok, _, err := unregisterClassW.Call(uintptr(unsafe.Pointer(className)), uintptr(module)); ok == 0 {
			slog.Warn("unregister tray window class", "error", nativeError("UnregisterClassW", err))
		}
	}()

	// A hidden top-level tool window, NOT HWND_MESSAGE: Explorer broadcasts
	// TaskbarCreated and session-end messages only to top-level windows.
	title, _ := windows.UTF16PtrFromString("Worklog")
	hwnd, _, err := createWindowExW.Call(0x80, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0,
		0, 0, 0, 0, 0, 0, uintptr(module), 0) // WS_EX_TOOLWINDOW; never shown
	if hwnd == 0 {
		return nativeError("create tray window", err)
	}
	t := &Tray{
		hwnd: hwnd, window: hwnd, icon: icon, taskbarCreated: uint32(taskbar),
		status: "● 服务启动中", onOpen: onOpen, onRestart: onRestart, onQuit: onQuit, shutdownDone: shutdownDone, autostart: autostart,
	}
	if len(onBackup) != 0 {
		t.onBackup = onBackup[0]
	}
	trayWindows.Store(hwnd, t)
	defer t.dispose()
	if err := t.createMenu(); err != nil {
		return err
	}
	// Explorer can be absent during login or restarting. Keep the native host
	// alive and retry rather than treating that as a fatal service failure.
	if err := t.addIcon(); err != nil {
		slog.Warn("add tray icon; will retry", "error", err)
		if err := t.startIconRetry(); err != nil {
			return err
		}
	}
	t.running = true
	defer func() { t.running = false }() // before dispose: do not leave WM_QUIT queued
	if onReady != nil {
		onReady(t)
	}

	var msg nativeMessage
	for {
		result, _, err := getMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(result) {
		case -1:
			return nativeError("tray GetMessageW", err)
		case 0: // WM_QUIT
			return nil
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// SetStatus replaces the disabled status label and its native colored dot.
// Legacy leading ● markers are omitted from the drawn text. It performs no
// lifecycle work. Updates coalesce until the UI thread processes them.
func (t *Tray) SetStatus(status string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.hwnd == 0 || t.closing {
		t.mu.Unlock()
		return
	}
	t.status = strings.ReplaceAll(status, "\x00", "\uFFFD")
	if t.statusPosted {
		t.mu.Unlock()
		return
	}
	t.statusPosted = true
	ok, _, err := postMessageW.Call(t.hwnd, wmTrayStatus, 0, 0)
	if ok == 0 {
		t.statusPosted = false
	}
	t.mu.Unlock()
	if ok == 0 {
		slog.Warn("post tray status", "error", nativeError("PostMessageW", err))
	}
}

// Close asynchronously tears down the native tray and stops RunTray. It is
// idempotent, safe from any goroutine, and does not call onQuit again.
func (t *Tray) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.hwnd == 0 || t.closing {
		t.mu.Unlock()
		return
	}
	ok, _, err := postMessageW.Call(t.hwnd, wmTrayClose, 0, 0)
	if ok != 0 {
		t.closing = true
	}
	t.mu.Unlock()
	if ok == 0 {
		slog.Error("post tray close", "error", nativeError("PostMessageW", err))
	}
}

func trayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	value, found := trayWindows.Load(hwnd)
	if !found {
		result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	t := value.(*Tray)
	if message == t.taskbarCreated {
		t.iconAdded = false // Explorer discarded the old registration.
		if err := t.addIcon(); err != nil {
			slog.Warn("restore tray icon", "error", err)
			if err := t.startIconRetry(); err != nil {
				slog.Error("schedule tray icon retry", "error", err)
			}
		}
		return 0
	}
	switch message {
	case wmTrayCallback:
		// Use the original NOTIFYICONDATA callback protocol (no NIM_SETVERSION),
		// so lParam is the mouse message and wParam is the icon ID.
		if wParam != 1 {
			return 0
		}
		switch uint32(lParam) {
		case wmLButtonDblClk:
			t.command(menuOpen)
		case wmRButtonUp, wmContextMenu:
			t.popupMenu()
		}
		return 0
	case wmCommand:
		t.command(uint32(wParam & 0xffff))
		return 0
	case wmMeasureItem:
		if lParam != 0 && t.measureStatus((*nativeMeasureItem)(unsafe.Pointer(lParam))) {
			return 1
		}
	case wmDrawItem:
		if lParam != 0 && t.drawStatus((*nativeDrawItem)(unsafe.Pointer(lParam))) {
			return 1
		}
	case wmTrayStatus:
		t.applyStatus()
		return 0
	case wmTrayClose:
		endMenu.Call() // also dismiss a popup's nested message loop
		if ok, _, err := destroyWindow.Call(hwnd); ok == 0 {
			slog.Error("destroy tray window", "error", nativeError("DestroyWindow", err))
			postQuitMessage.Call(0) // let deferred cleanup attempt destruction again
		}
		return 0
	case wmClose:
		t.command(menuQuit)
		return 0
	case wmQueryEndSession:
		return 1 // Never veto Windows logoff/shutdown.
	case wmEndSession:
		if wParam != 0 {
			t.command(menuQuit)
			// Windows may terminate the process as soon as this message returns.
			// Give cleanup a bounded chance without blocking ordinary menu input.
			if t.shutdownDone != nil {
				timer := time.NewTimer(4 * time.Second)
				select {
				case <-t.shutdownDone:
				case <-timer.C:
					slog.Warn("session_shutdown_wait_expired")
				}
				timer.Stop()
			}
		}
		return 0
	case wmTimer:
		if wParam == iconRetryTimer {
			// Avoid logging every poll while Explorer is unavailable.
			_ = t.addIcon()
			return 0
		}
	case wmDestroy:
		t.mu.Lock()
		t.hwnd, t.closing = 0, true
		t.mu.Unlock()
		t.stopIconRetry()
		t.removeIcon()
		t.destroyed = true
		if t.running {
			postQuitMessage.Call(0)
		}
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (t *Tray) command(id uint32) {
	switch id {
	case menuOpen:
		if t.onOpen != nil {
			t.onOpen()
		}
	case menuRestart:
		if t.onRestart != nil {
			t.onRestart()
		}
	case menuExport, menuImport:
		if !t.quitRequested {
			t.requestBackup(id == menuImport)
		}
	case menuAutostart:
		if t.autostart == nil || t.quitRequested {
			return
		}
		enabled, err := t.autostart.Enabled()
		if err == nil {
			err = t.autostart.SetEnabled(!enabled)
		}
		if err != nil {
			slog.Error("autostart_update_failed", "error", err)
			go func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				ShowError("无法修改开机启动设置，请查看 worklog.log。")
			}()
		} else {
			slog.Info("autostart_updated", "enabled", !enabled)
		}
		t.refreshAutostart()
	case menuQuit:
		if !t.quitRequested {
			t.quitRequested = true
			if t.onQuit != nil {
				t.onQuit()
			}
		}
	}
}

func (t *Tray) createMenu() error {
	menu, _, err := createPopupMenu.Call()
	if menu == 0 {
		return nativeError("create tray menu", err)
	}
	t.menu = menu // dispose also covers partial initialization
	t.menuStatusText = t.status
	items := []struct {
		id    uint32
		flags uint32
		label string
	}{
		{menuOpen, 0, "打开 Worklog"},
		{0, mfSeparator, ""},
		{menuStatus, mfDisabled | mfOwnerDraw, ""},
		{0, mfSeparator, ""},
		{menuImport, 0, "导入备份…"},
		{menuExport, 0, "导出备份…"},
		{0, mfSeparator, ""},
		{menuAutostart, 0, "开机启动"},
		{0, mfSeparator, ""},
		{menuRestart, 0, "重启服务"},
		{menuQuit, 0, "退出 Worklog"},
	}
	for _, item := range items {
		if (item.id == menuExport || item.id == menuImport) && t.onBackup == nil {
			item.flags = mfDisabled
		}
		label := menuText(item.label)
		if item.id == menuStatus {
			label = nil // owner-draw item data, never a Go pointer
		}
		if ok, _, err := appendMenuW.Call(menu, uintptr(item.flags), uintptr(item.id), uintptr(unsafe.Pointer(label))); ok == 0 {
			return nativeError("append tray menu item", err)
		}
	}
	t.refreshAutostart()
	return nil
}

// Read back the registry instead of maintaining a separate preference, including
// when another program has changed it since the last menu was shown.
func (t *Tray) refreshAutostart() {
	if t.menu == 0 || t.destroyed {
		return
	}
	flags := uintptr(0)
	if t.autostart == nil {
		flags = mfDisabled
	} else if enabled, err := t.autostart.Enabled(); err != nil {
		slog.Warn("autostart_read_failed", "error", err)
		flags = mfDisabled
	} else if enabled {
		flags = mfChecked
	}
	label := menuText("开机启动")
	if ok, _, err := modifyMenuW.Call(t.menu, menuAutostart, flags, menuAutostart, uintptr(unsafe.Pointer(label))); ok == 0 {
		slog.Warn("update autostart menu", "error", nativeError("ModifyMenuW", err))
	}
}

func (t *Tray) popupMenu() {
	t.refreshAutostart()
	var point nativePoint
	if ok, _, err := getCursorPos.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
		slog.Warn("get tray menu position", "error", nativeError("GetCursorPos", err))
		return
	}
	// Required for correct dismissal when clicking outside a notification menu.
	setForegroundWindow.Call(t.window)
	command, _, _ := trackPopupMenu.Call(t.menu, 0x182, // TPM_RETURNCMD | TPM_NONOTIFY | TPM_RIGHTBUTTON
		uintptr(point.X), uintptr(point.Y), 0, t.window, 0)
	if t.destroyed { // Close may run inside TrackPopupMenu's nested loop.
		return
	}
	if ok, _, err := postMessageW.Call(t.window, wmNull, 0, 0); ok == 0 {
		slog.Warn("post tray menu dismissal", "error", nativeError("PostMessageW", err))
	}
	if command != 0 {
		t.command(uint32(command))
	}
}

func (t *Tray) applyStatus() {
	t.mu.Lock()
	status := t.status
	t.statusPosted = false
	t.mu.Unlock()
	t.menuStatusText = status
	// MF_BYCOMMAND is zero. Retain owner-draw and disabled flags; item data
	// stays zero because callbacks identify this row by its command ID.
	if ok, _, err := modifyMenuW.Call(t.menu, menuStatus, mfDisabled|mfOwnerDraw, menuStatus, 0); ok == 0 {
		slog.Warn("update tray menu status", "error", nativeError("ModifyMenuW", err))
	}
	if t.iconAdded {
		data := t.iconData(status)
		data.Flags = nifTip
		if ok, _, err := shellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data))); ok == 0 {
			slog.Warn("update tray tooltip", "error", nativeError("Shell_NotifyIconW", err))
		}
	}
}

func (t *Tray) iconData(status string) nativeNotifyIcon {
	data := nativeNotifyIcon{
		Size: uint32(unsafe.Sizeof(nativeNotifyIcon{})), Window: t.window,
		ID: 1, Flags: nifMessage | nifIcon | nifTip, Callback: wmTrayCallback, Icon: t.icon,
	}
	fillUTF16(data.Tip[:], "Worklog\n"+status)
	return data
}

func (t *Tray) addIcon() error {
	if t.iconAdded || t.destroyed {
		return nil
	}
	t.mu.Lock()
	status := t.status
	t.mu.Unlock()
	data := t.iconData(status)
	if ok, _, err := shellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data))); ok == 0 {
		return nativeError("Shell_NotifyIconW(NIM_ADD)", err)
	}
	t.iconAdded = true
	t.stopIconRetry()
	return nil
}

func (t *Tray) removeIcon() {
	if !t.iconAdded {
		return
	}
	data := nativeNotifyIcon{Size: uint32(unsafe.Sizeof(nativeNotifyIcon{})), Window: t.window, ID: 1}
	if ok, _, err := shellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data))); ok == 0 {
		slog.Warn("remove tray icon", "error", nativeError("Shell_NotifyIconW(NIM_DELETE)", err))
	}
	t.iconAdded = false
}

func (t *Tray) startIconRetry() error {
	if t.retryTimer {
		return nil
	}
	if timer, _, err := setTimer.Call(t.window, iconRetryTimer, 2000, 0); timer == 0 {
		return nativeError("start tray icon retry timer", err)
	}
	t.retryTimer = true
	return nil
}

func (t *Tray) stopIconRetry() {
	if t.retryTimer {
		if ok, _, err := killTimer.Call(t.window, iconRetryTimer); ok == 0 {
			slog.Warn("stop tray icon retry timer", "error", nativeError("KillTimer", err))
		}
		t.retryTimer = false
	}
}

func (t *Tray) dispose() {
	// Invalidate the cross-thread posting target before destroying any native
	// resources, so a late caller can never post to a recycled window handle.
	t.mu.Lock()
	t.hwnd, t.closing = 0, true
	t.mu.Unlock()
	t.stopIconRetry()
	t.removeIcon()
	if !t.destroyed {
		if ok, _, err := destroyWindow.Call(t.window); ok == 0 {
			slog.Warn("clean up tray window", "error", nativeError("DestroyWindow", err))
		}
	}
	trayWindows.Delete(t.window)
	if t.menu != 0 {
		if ok, _, err := destroyMenu.Call(t.menu); ok == 0 {
			slog.Warn("destroy tray menu", "error", nativeError("DestroyMenu", err))
		}
		t.menu = 0
	}
}

func menuText(text string) *uint16 {
	// Menu ampersands otherwise mark accelerators instead of literal text.
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\x00", "\uFFFD"), "&", "&&")
	ptr, _ := windows.UTF16PtrFromString(text)
	return ptr
}

// fillUTF16 always NUL terminates and never truncates a surrogate pair.
func fillUTF16(dst []uint16, text string) {
	clear(dst)
	if len(dst) == 0 {
		return
	}
	encoded := utf16.Encode([]rune(strings.ReplaceAll(text, "\x00", "\uFFFD")))
	n := min(len(encoded), len(dst)-1)
	if n > 0 && n < len(encoded) && encoded[n-1] >= 0xD800 && encoded[n-1] <= 0xDBFF {
		n--
	}
	copy(dst, encoded[:n])
}
