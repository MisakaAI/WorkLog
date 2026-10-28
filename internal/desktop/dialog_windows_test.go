//go:build windows

package desktop

import (
	"testing"
	"unsafe"
)

func TestDialogNativeLayout(t *testing.T) {
	p := unsafe.Sizeof(uintptr(0))
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"BROWSEINFOW size", unsafe.Sizeof(nativeBrowseInfo{}), 8 * p},
		{"BROWSEINFOW.lpfn", unsafe.Offsetof(nativeBrowseInfo{}.Callback), 5 * p},
		{"OPENFILENAMEW size", unsafe.Sizeof(nativeOpenFileName{}), 16*p + 24},
		{"OPENFILENAMEW.lpstrFile", unsafe.Offsetof(nativeOpenFileName{}.File), 5*p + 8},
		{"OPENFILENAMEW.Flags", unsafe.Offsetof(nativeOpenFileName{}.Flags), 11*p + 8},
		{"OPENFILENAMEW.FlagsEx", unsafe.Offsetof(nativeOpenFileName{}.FlagsEx), 16*p + 20},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}

func TestBackupRequestBusyOrUnavailable(t *testing.T) {
	tray := &Tray{}
	tray.requestBackup(false) // No handler: no dialog and no busy state.
	if tray.backupBusy.Load() {
		t.Fatal("missing callback acquired busy guard")
	}
	tray.onBackup = func(BackupRequest) { t.Error("busy request dispatched") }
	tray.backupBusy.Store(true)
	tray.requestBackup(true)
	if !tray.backupBusy.Load() {
		t.Fatal("busy request released another operation's guard")
	}
}

// Both the historical signature and the optional callback must compile.
func compileTrayBackupAPI() {
	_ = RunTray(nil, nil, nil, nil, nil, nil)
	_ = RunTray(nil, nil, nil, nil, nil, nil, func(BackupRequest) {})
}
