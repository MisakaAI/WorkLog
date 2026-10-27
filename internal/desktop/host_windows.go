//go:build windows

// Package desktop provides the native Windows desktop host. It does not own the
// application's service lifecycle; callbacks should only enqueue work.
package desktop

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Instance holds a per-user, per-session mutex on a dedicated locked OS thread.
// Ownership, not handle existence, elects the primary, so a crashed primary can
// be replaced even while another process retains a mutex handle. Do not copy
// an Instance after first use.
type Instance struct {
	mu        sync.Mutex // also protects the event during a bounded native wait
	event     windows.Handle
	closed    bool
	closeErr  error
	ownerStop chan struct{}
	ownerDone chan struct{}
	ownerErr  error // written by the owner; read only after ownerDone closes
}

// AcquireInstance returns (instance, true, nil) for the primary process, or
// (nil, false, nil) after notifying an existing primary to open its UI. All
// handles have default security descriptors and live in the local session's
// namespace; the user SID is hashed so different users do not share names.
func AcquireInstance() (*Instance, bool, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, false, fmt.Errorf("query instance user SID: %w", err)
	}
	digest := sha256.Sum256([]byte(user.User.Sid.String()))
	return acquireNamedInstance(fmt.Sprintf(`Local\Worklog.%x`, digest))
}

// acquireNamedInstance also lets native tests exercise election with isolated
// names, without notifying a real Worklog process running in the same session.
func acquireNamedInstance(base string) (*Instance, bool, error) {
	eventName, err := windows.UTF16PtrFromString(base + ".Open")
	if err != nil {
		return nil, false, err
	}
	mutexName, err := windows.UTF16PtrFromString(base + ".Instance")
	if err != nil {
		return nil, false, err
	}

	// Every contender opens the auto-reset event BEFORE the mutex. Therefore
	// once a primary has been elected, its event already exists and is held
	// open. A launch during startup can signal it even before Watch starts.
	// Never reset an existing event: that would lose a secondary's request.
	event, err := windows.CreateEvent(nil, 0, 0, eventName)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create instance event: %w", err)
	}
	i := &Instance{event: event, ownerStop: make(chan struct{}), ownerDone: make(chan struct{})}
	type election struct {
		primary bool
		err     error
	}
	ready := make(chan election, 1)
	go func() {
		defer close(i.ownerDone) // after native handle cleanup and thread unlock
		runtime.LockOSThread()
		unlockThread := true
		defer func() {
			if unlockThread {
				runtime.UnlockOSThread()
			}
		}()
		mutex, err := windows.CreateMutex(nil, false, mutexName)
		if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			ready <- election{err: fmt.Errorf("create instance mutex: %w", err)}
			return
		}
		defer func() {
			i.ownerErr = errors.Join(i.ownerErr, closeInstanceHandle("mutex", mutex))
		}()
		result, err := windows.WaitForSingleObject(mutex, 0)
		if err != nil {
			ready <- election{err: fmt.Errorf("acquire instance mutex: %w", err)}
			return
		}
		switch result {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			ready <- election{primary: true}
			<-i.ownerStop
			if err := windows.ReleaseMutex(mutex); err != nil {
				i.ownerErr = fmt.Errorf("release instance mutex: %w", err)
				// Never return a potentially still-owning thread to Go's pool.
				// Exiting a locked goroutine makes Go retire its OS thread.
				unlockThread = false
			}
		case uint32(windows.WAIT_TIMEOUT):
			if err := windows.SetEvent(event); err != nil {
				ready <- election{err: fmt.Errorf("notify primary instance: %w", err)}
			} else {
				ready <- election{}
			}
		default:
			ready <- election{err: fmt.Errorf("unexpected instance mutex wait result: %d", result)}
		}
	}()
	outcome := <-ready
	if !outcome.primary {
		<-i.ownerDone
		return nil, false, errors.Join(outcome.err, i.ownerErr, closeInstanceHandle("event", event))
	}
	return i, true, nil
}

// Watch waits for open requests until ctx is cancelled or Close is called.
// Call it once for the primary instance. Requests may coalesce, as opening an
// already open UI is idempotent. open must be nonblocking (enqueue work only).
func (i *Instance) Watch(ctx context.Context, open func()) {
	if i == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		i.mu.Lock()
		if i.closed {
			i.mu.Unlock()
			return
		}
		// Holding mu prevents CloseHandle while WaitForSingleObject is using
		// the handle (undefined on Windows). Cancellation/Close latency is
		// bounded by this 200ms timeout, with no IPC listener.
		result, err := windows.WaitForSingleObject(i.event, 200)
		i.mu.Unlock()
		if err != nil {
			slog.Error("wait for instance notification", "error", err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			if open != nil {
				open()
			}
		case uint32(windows.WAIT_TIMEOUT):
			// Poll for cancellation without modifying the shared event.
		default:
			slog.Error("unexpected instance wait result", "result", result)
			return
		}
	}
}

// Close stops Watch, closes the event, and waits for the owning OS thread to
// release and close the mutex. It is idempotent and safe to call concurrently
// or while Watch is running (which stops within one poll).
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return i.closeErr
	}
	i.closed = true
	// Release ownership last, after Watch can no longer use the event. The
	// owner does not take mu, so holding it also serializes concurrent Close.
	eventErr := closeInstanceHandle("event", i.event)
	i.event = 0
	close(i.ownerStop)
	<-i.ownerDone
	i.closeErr = errors.Join(eventErr, i.ownerErr)
	return i.closeErr
}

func closeInstanceHandle(kind string, handle windows.Handle) error {
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close instance %s handle: %w", kind, err)
	}
	return nil
}

var shellExecuteW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")

// OpenBrowser opens an absolute HTTP(S) URL through the user's registered
// browser. No shell, command interpreter, or child-process command line is used.
func OpenBrowser(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("parse browser URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("browser URL must be an absolute HTTP(S) URL")
	}
	file, err := windows.UTF16PtrFromString(address)
	if err != nil {
		return fmt.Errorf("encode browser URL: %w", err)
	}
	verb, _ := windows.UTF16PtrFromString("open")

	// Some ShellExecute handlers require COM. Balance successful S_OK and
	// S_FALSE initializations on this same OS thread; an existing different
	// apartment is usable and must not be uninitialized by us.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	switch {
	case err == nil, errors.Is(err, syscall.Errno(1)): // S_OK, S_FALSE
		defer windows.CoUninitialize()
	case errors.Is(err, syscall.Errno(0x80010106)): // RPC_E_CHANGED_MODE
	default:
		return fmt.Errorf("initialize browser COM apartment: %w", err)
	}
	result, _, callErr := shellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, windows.SW_SHOWNORMAL)
	if result <= 32 {
		// ShellExecute's return value, not GetLastError, defines success.
		return fmt.Errorf("ShellExecuteW failed (code %d): %w", result, nativeError("open browser", callErr))
	}
	return nil
}

// ShowError displays a fatal startup error before a tray exists. It blocks
// until dismissed; do not call it from the tray's message-loop callbacks.
func ShowError(message string) {
	text, _ := windows.UTF16PtrFromString(strings.ReplaceAll(message, "\x00", "\uFFFD"))
	title, _ := windows.UTF16PtrFromString("Worklog")
	if _, err := windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_TASKMODAL|windows.MB_SETFOREGROUND); err != nil {
		slog.Error("show startup error dialog", "error", err)
	}
}

func nativeError(operation string, err error) error {
	if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
