//go:build darwin

package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Instance uses a persistent lock inode. Never unlink the lock: contenders may
// still have it open. The kernel releases flock even after an unclean exit.
type Instance struct {
	mu      sync.Mutex
	file    *os.File
	request string
	done    chan struct{}
}

// AcquireInstance must be called with a private, validated per-user directory,
// before opening configuration, logs, tokens or the database. Notifications are
// empty exclusive-create markers, not URLs, PIDs, signals or network messages.
// An optional false notify value makes background login launches exit silently
// if another instance already owns the lock. Active launches notify by default.
func AcquireInstance(root string, notify ...bool) (*Instance, bool, error) {
	fd, err := unix.Open(filepath.Join(root, ".instance.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, false, err
	}
	f := os.NewFile(uintptr(fd), "instance lock")
	var info unix.Stat_t
	if err = unix.Fstat(fd, &info); err != nil {
		f.Close()
		return nil, false, fmt.Errorf("inspect instance lock: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != uint32(os.Geteuid()) || info.Mode&0077 != 0 || info.Nlink != 1 {
		f.Close()
		return nil, false, errors.New("instance lock must be a private regular file with a single hard link")
	}
	request := filepath.Join(root, ".open-request")
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return &Instance{file: f, request: request, done: make(chan struct{})}, true, nil
	}
	f.Close()
	if !errors.Is(err, unix.EWOULDBLOCK) {
		return nil, false, err
	}
	if len(notify) > 0 && !notify[0] {
		return nil, false, nil
	}
	// A marker survives startup until Watch removes it. Concurrent launches
	// coalesce; removing it BEFORE the callback preserves later notifications.
	notification, err := os.OpenFile(request, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return nil, false, notification.Close()
}

// Watch is called once by the primary. open must only enqueue work and return;
// it must not call Close. Cancellation and Close both stop the watcher.
func (i *Instance) Watch(ctx context.Context, open func()) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.done:
			return
		case <-ticker.C:
			i.mu.Lock()
			if i.file != nil && ctx.Err() == nil && os.Remove(i.request) == nil && open != nil {
				open()
			}
			i.mu.Unlock()
		}
	}
}

func (i *Instance) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.file == nil {
		return nil
	}
	close(i.done)
	err := i.file.Close()
	i.file = nil
	return err
}
