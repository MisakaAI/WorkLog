//go:build darwin

package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMacBackgroundSecondaryDoesNotOpenBrowser(t *testing.T) {
	root := t.TempDir()
	i, primary, err := AcquireInstance(root)
	if err != nil || !primary {
		t.Fatal("primary election", err)
	}
	defer i.Close()
	secondary, primary, err := AcquireInstance(root, false)
	if err != nil || primary || secondary != nil {
		t.Fatal("background secondary", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".open-request")); !os.IsNotExist(err) {
		t.Fatal("background launch notified existing instance", err)
	}
	if _, primary, err := AcquireInstance(root); err != nil || primary {
		t.Fatal("active secondary", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".open-request")); err != nil {
		t.Fatal("active secondary failed to notify", err)
	}
}

func TestMacInstanceNotificationBeforeWatch(t *testing.T) {
	root := t.TempDir()
	i, primary, err := AcquireInstance(root)
	if err != nil || !primary {
		t.Fatalf("election: primary=%v err=%v", primary, err)
	}
	defer i.Close()
	for n := 0; n < 3; n++ {
		secondary, primary, err := AcquireInstance(root)
		if err != nil || primary || secondary != nil {
			t.Fatalf("secondary: primary=%v err=%v", primary, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() { defer close(done); i.Watch(ctx, func() { opened <- struct{}{} }) }()
	select {
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("lost startup notification")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch ignored cancellation")
	}
	if _, err := os.Stat(filepath.Join(root, ".open-request")); !os.IsNotExist(err) {
		t.Fatalf("marker not consumed: %v", err)
	}
}

func TestMacConcurrentInstanceElectionAndClose(t *testing.T) {
	root := t.TempDir()
	type result struct {
		instance *Instance
		primary  bool
		err      error
	}
	results := make(chan result, 16)
	start := make(chan struct{})
	for n := 0; n < cap(results); n++ {
		go func() { <-start; i, p, err := AcquireInstance(root); results <- result{i, p, err} }()
	}
	close(start)
	var instance *Instance
	count := 0
	for n := 0; n < cap(results); n++ {
		r := <-results
		if r.err != nil {
			t.Error(r.err)
		}
		if r.primary {
			count++
			instance = r.instance
			defer r.instance.Close()
		}
	}
	if count != 1 {
		t.Fatalf("elected %d primaries", count)
	}
	done := make(chan struct{})
	go func() { defer close(done); instance.Watch(context.Background(), func() {}) }()
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch ignored Close")
	}
	replacement, primary, err := AcquireInstance(root)
	if replacement != nil {
		defer replacement.Close()
	}
	if err != nil || !primary {
		t.Fatalf("persistent inode blocked replacement: %v", err)
	}
}

func TestMacInstanceRecoversAfterProcessExit(t *testing.T) {
	root := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestMacInstanceExitHelper$")
	child.Env = append(os.Environ(), "WORKLOG_TEST_INSTANCE_DIR="+root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v\n%s", err, output)
	}
	i, primary, err := AcquireInstance(root)
	if i != nil {
		defer i.Close()
	}
	if err != nil || !primary {
		t.Fatalf("crash left ownership behind: %v", err)
	}
}

func TestMacInstanceExitHelper(t *testing.T) {
	root := os.Getenv("WORKLOG_TEST_INSTANCE_DIR")
	if root == "" {
		return
	}
	i, primary, err := AcquireInstance(root)
	if err != nil || !primary || i == nil {
		t.Fatal("helper election", err)
	}
	os.Exit(0) // Deliberately bypass Close to model an unclean process exit.
}

func TestMacInstanceRejectsUnsafeLock(t *testing.T) {
	for _, kind := range []string{"symlink", "shared", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			lock := filepath.Join(root, ".instance.lock")
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, lock)
			case "shared":
				err = os.WriteFile(lock, nil, 0600)
				if err == nil {
					err = os.Chmod(lock, 0644)
				}
			case "hardlink":
				err = os.Link(target, lock)
			case "fifo":
				err = unix.Mkfifo(lock, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			i, _, err := AcquireInstance(root)
			if i != nil {
				i.Close()
			}
			if err == nil {
				t.Fatal("accepted unsafe lock")
			}
			if content, _ := os.ReadFile(target); string(content) != "keep" {
				t.Fatal("changed linked target")
			}
		})
	}
}

func TestMacInstanceCrossProcessNotification(t *testing.T) {
	root := t.TempDir()
	i, primary, err := AcquireInstance(root)
	if err != nil || !primary {
		t.Fatal(err)
	}
	defer i.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestMacInstanceSecondaryHelper$")
	child.Env = append(os.Environ(), "WORKLOG_TEST_SECONDARY_DIR="+root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("secondary: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, ".open-request")); err != nil {
		t.Fatal("no cross-process notification", err)
	}
}

func TestMacInstanceSecondaryHelper(t *testing.T) {
	root := os.Getenv("WORKLOG_TEST_SECONDARY_DIR")
	if root == "" {
		return
	}
	i, primary, err := AcquireInstance(root)
	if err != nil || primary || i != nil {
		t.Fatal(fmt.Sprintf("secondary: primary=%v err=%v", primary, err))
	}
}
