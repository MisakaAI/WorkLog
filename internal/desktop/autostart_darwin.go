//go:build darwin && cgo

package desktop

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const launchAgentLabel = "io.github.misakaai.worklog.desktop"

// Autostart manages one per-user LaunchAgent. Only an explicit SetEnabled(true)
// writes it; creating this object or querying it never registers login startup.
// No shell, helper process, KeepAlive job or developer signing is required.
type Autostart struct {
	mu         sync.Mutex
	path       string
	executable string
	contents   []byte
}

func NewAutostart(executable, conf string) (*Autostart, error) {
	for name, path := range map[string]string{"executable": executable, "configuration": conf} {
		if name == "configuration" && path == "" {
			continue
		}
		if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
			return nil, fmt.Errorf("%s must be an absolute path without NUL", name)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(home) {
		return nil, errors.New("user home must be an absolute path")
	}
	args := []string{executable, "--autostart"}
	if conf != "" {
		args = append(args, "--conf", conf)
	}
	var data bytes.Buffer
	data.WriteString(xml.Header + `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	data.WriteString(`<plist version="1.0"><dict>` + "\n<key>Label</key><string>" + launchAgentLabel + "</string>\n<key>ProgramArguments</key><array>\n")
	for _, arg := range args {
		data.WriteString("<string>")
		if err := xml.EscapeText(&data, []byte(arg)); err != nil {
			return nil, err
		}
		data.WriteString("</string>\n")
	}
	data.WriteString("</array>\n<key>RunAtLoad</key><true/>\n<key>KeepAlive</key><false/>\n<key>LimitLoadToSessionType</key><string>Aqua</string>\n<key>AssociatedBundleIdentifiers</key><array><string>" + launchAgentLabel + "</string></array>\n</dict></plist>\n")
	return &Autostart{path: filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), executable: executable, contents: data.Bytes()}, nil
}

// Enabled reports whether our private file matches this executable/config.
// System Settings or launchd policy may independently prevent execution.
func (a *Autostart) Enabled() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := readLaunchAgent(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil && bytes.Equal(data, a.contents), err
}

func (a *Autostart) SetEnabled(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := readLaunchAgent(a.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !enabled && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	dir := filepath.Dir(a.path)
	if enabled {
		info, err := os.Stat(a.executable)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("login executable must be a regular file")
		}
		var volume unix.Statfs_t
		if err := unix.Statfs(a.executable, &volume); err != nil {
			return err
		}
		if volume.Flags&unix.MNT_RDONLY != 0 || strings.Contains(a.executable, "/AppTranslocation/") {
			return errors.New("请先将 worklog.app 拖入 Applications，再启用登录后启动")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if err := validateLaunchAgentDir(dir); err != nil {
		return err
	}
	if !enabled {
		return os.Remove(a.path)
	}
	file, err := os.CreateTemp(dir, ".worklog-login-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := CheckPrivateFile(file); err != nil {
		return err
	}
	if _, err := file.Write(a.contents); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), a.path)
}

func readLaunchAgent(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if err := CheckPrivateFile(file); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, 64<<10))
}

func validateLaunchAgentDir(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Uid != uint32(os.Geteuid()) || info.Mode&0022 != 0 {
		return errors.New("LaunchAgents directory must be owned by the current user and not writable by others")
	}
	acl, err := hasExtendedACL(fd)
	if err != nil {
		return err
	}
	if acl {
		return errors.New("LaunchAgents directory must not have extended ACLs")
	}
	return nil
}
