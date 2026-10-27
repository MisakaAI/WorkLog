//go:build windows

// This packaging-only supervisor runs the original, unmodified cmd/worklog.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "desktop supervisor:", err)
		os.Exit(1)
	}
}

func run() (err error) {
	if len(os.Args) != 3 {
		return errors.New("expected backend executable and configuration file")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create lifetime job: %w", err)
	}
	// A non-inheritable handle is owned ONLY by this supervisor. Closing it,
	// including on a crash, kills every process in the job. Join BEFORE spawning
	// so the child inherits membership atomically (no spawn/assign race).
	joined := false
	defer func() {
		// Closing a job containing this supervisor also terminates us. Report
		// failures first; main may never get a chance to print its return error.
		if joined && err != nil {
			fmt.Fprintln(os.Stderr, "desktop supervisor:", err)
		}
		_ = windows.CloseHandle(job)
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	// SetInformationJobObject accepts uintptr without a uintptrescapes directive.
	// Pin the structure so stack growth in its Go wrapper cannot invalidate the
	// address before Windows reads it. KeepAlive alone would not prevent movement.
	var limitsPin runtime.Pinner
	limitsPin.Pin(&limits)
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	limitsPin.Unpin()
	if err != nil {
		return fmt.Errorf("configure lifetime job: %w", err)
	}
	if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return fmt.Errorf("join lifetime job (host policy may forbid nested jobs): %w", err)
	}
	joined = true
	// stdin is an anonymous pipe from Electron, never a renderer IPC channel.
	// Electron termination closes it even if its JS cleanup could not run.
	parentGone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(parentGone)
	}()
	select {
	case <-parentGone:
		return nil
	default:
	}
	cmd := exec.Command(os.Args[1], "--conf", os.Args[2], "serve")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start backend: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(75 * time.Second)
	defer timeout.Stop()
	ready := false
	for {
		select {
		case <-parentGone:
			// Windows does not provide POSIX SIGTERM to this console-less child.
			// Job closure forcibly stops it; SQLite WAL is recovered on next open.
			return nil
		case err = <-done:
			return fmt.Errorf("backend exited unexpectedly: %v", err)
		case <-timeout.C:
			if !ready {
				return errors.New("backend did not bind a loopback port within 75 seconds")
			}
		case <-ticker.C:
			if ready {
				continue
			}
			port, err := listeningPort(uint32(cmd.Process.Pid))
			if err != nil {
				return fmt.Errorf("read backend TCP listener: %w", err)
			}
			if port == 0 {
				continue
			}
			if err = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"type": "listening", "port": port, "pid": cmd.Process.Pid,
			}); err != nil {
				return fmt.Errorf("report listener: %w", err)
			}
			ready = true
			timeout.Stop()
		}
	}
}

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// Query the OS-owned socket, never reserve/probe/release a prospective port.
// MIB_TCP_TABLE_OWNER_PID_LISTENER = 3, AF_INET = 2. Each Windows DWORD is
// little-endian except the address/port byte sequences, which are network order.
func listeningPort(pid uint32) (uint16, error) {
	var size uint32
	for attempts := 0; attempts < 8; attempts++ {
		var table []byte
		var ptr uintptr
		if size != 0 {
			if size > 64<<20 {
				return 0, errors.New("unexpected TCP table size")
			}
			table = make([]byte, size)
			ptr = uintptr(unsafe.Pointer(&table[0]))
		}
		status, _, _ := getExtendedTCPTable.Call(ptr, uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0)
		runtime.KeepAlive(table)
		if windows.Errno(status) == windows.ERROR_INSUFFICIENT_BUFFER {
			continue
		}
		if status != 0 {
			return 0, windows.Errno(status)
		}
		return parseTCPTable(table, pid)
	}
	return 0, errors.New("TCP table kept changing; retry limit exceeded")
}
