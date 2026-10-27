//go:build windows && desktop

// Worklog is a single-process tray host. No backend child processes are started.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata"

	web "worklog/Web"
	"worklog/internal/app"
	"worklog/internal/config"
	"worklog/internal/desktop"
	"worklog/internal/diagnostics"
	"worklog/internal/service"
	"worklog/internal/storage"
	"worklog/internal/transport/rest"
)

func main() {
	if err := run(); err != nil {
		desktop.ShowError("Worklog 无法启动。\n" + err.Error())
		os.Exit(1)
	}
}

func run() error {
	// Acquire before touching the database, token, configuration or log files.
	instance, primary, err := desktop.AcquireInstance()
	if err != nil {
		return fmt.Errorf("单实例初始化失败: %w", err)
	}
	if !primary {
		return nil
	}
	defer instance.Close()

	flags := flag.NewFlagSet("Worklog", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // Release has no console.
	conf := flags.String("conf", "", "configuration JSON file or directory")
	background := flags.Bool("autostart", false, "start in the tray without automatically opening the browser")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() != 0 {
		return fmt.Errorf("用法: %s [--conf 配置文件或目录]", filepath.Base(os.Args[0]))
	}
	var emptyConf bool
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "conf" && f.Value.String() == "" {
			emptyConf = true
		}
	})
	if emptyConf {
		return errors.New("--conf 不能为空")
	}
	if *conf != "" {
		*conf, err = filepath.Abs(*conf)
		if err != nil {
			return err
		}
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	root, err = filepath.Abs(filepath.Join(root, "WorklogDesktop"))
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err = desktop.SecureDataDir(root); err != nil {
		return err
	}
	// Existing relative database/token/config paths retain their semantics, but
	// never depend on Explorer's or a shortcut's working directory.
	if err = os.Chdir(root); err != nil {
		return err
	}
	if *conf == "" {
		if _, err = os.Stat("config.json"); err == nil {
			*conf = "config.json"
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	logFile, err := openLog(root)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	slog.SetDefault(logger)
	logger.Info("desktop_starting", "data_directory", root)
	if *conf != "" {
		*conf, err = filepath.Abs(*conf)
		if err != nil {
			return err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	autostart, err := desktop.NewAutostart(executable, *conf)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Buffered/coalescing commands keep the Win32 message thread responsive.
	open := make(chan struct{}, 1)
	restart := make(chan struct{}, 1)
	enqueue := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		instance.Watch(ctx, func() { enqueue(open) })
	}()
	workerDone := make(chan struct{})
	browserRequests := make(chan *app.HTTP, 1)
	// Shell/DDE handlers can block. Never let an external browser delay service
	// restart or cleanup; the browser worker is intentionally not joined on exit.
	go runBrowser(ctx, browserRequests, logger)
	started := false // onReady is called synchronously on the tray thread.
	err = desktop.RunTray(func(tray *desktop.Tray) {
		started = true
		go func() {
			defer close(workerDone)
			defer tray.Close()
			runService(ctx, *conf, logger, tray, open, restart, browserRequests, !*background)
		}()
	}, func() { enqueue(open) }, func() { enqueue(restart) }, cancel, workerDone, autostart)
	cancel()
	if started {
		<-workerDone
	}
	<-watchDone
	if err != nil {
		logger.Error("tray_failed", diagnostics.Fields(err)...)
	}
	logger.Info("desktop_stopped")
	return err
}

func openLog(root string) (*os.File, error) {
	path := filepath.Join(root, "worklog.log")
	// Bound accumulated request logs without requiring another background task.
	if info, err := os.Stat(path); err == nil && info.Size() >= 5<<20 {
		_ = os.Remove(path + ".1")
		if err = os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

// All initialization, restart and cleanup operations are serialized here.
// The database, service, API handler and their process-scoped resources are
// created once, not once per HTTP restart.
func runService(ctx context.Context, conf string, logger *slog.Logger, tray *desktop.Tray, open, restart <-chan struct{}, browserRequests chan<- *app.HTTP, autoOpen bool) {
	var store *storage.Store
	var server *app.HTTP
	status := func(label string) {
		tray.SetStatus(label)
		logger.Info("desktop_service_state", "state", label)
	}
	defer func() {
		if server != nil {
			shutdownHTTP(server, logger)
		}
		if store != nil {
			if err := store.Close(); err != nil {
				logger.Error("database_close_failed", diagnostics.Fields(err)...)
			}
		}
		status("● 服务已停止")
	}()
	start := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if server == nil {
			c, err := config.LoadDesktop(conf)
			if err != nil {
				return diagnostics.Wrap("load_configuration", err)
			}
			startup, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			store, err = storage.Open(startup, c.Driver, c.DSN)
			if err != nil {
				return diagnostics.Wrap("initialize_database", err)
			}
			loc, _ := time.LoadLocation(c.Timezone)
			svc := service.New(store, loc)
			server = app.NewHTTP(c.Addr, rest.NewWithStaticFS(svc, c, logger, web.Assets()), logger)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return server.Start()
	}
	openBrowser := func() {
		if ctx.Err() != nil || server == nil || server.URL() == "" {
			return
		}
		select {
		case browserRequests <- server:
		default: // Coalesce requests while the shell is busy.
		}
	}
	var done <-chan error
	status("● 服务启动中")
	if err := start(); err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Error("desktop_start_failed", diagnostics.Fields(err)...)
		status("● 服务异常")
	} else {
		status("● 服务运行中")
		done = server.Done()
		// Only the initial active launch opens automatically. Login autostart
		// stays in the tray, unless another launch explicitly requested a page.
		select {
		case <-open:
			autoOpen = true
		default:
		}
		if autoOpen {
			openBrowser()
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-open:
			openBrowser()
		case <-restart:
			if ctx.Err() != nil {
				return
			}
			status("● 服务重启中")
			done = nil
			if server != nil {
				shutdownHTTP(server, logger)
			}
			if err := start(); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("desktop_restart_failed", diagnostics.Fields(err)...)
				status("● 服务异常")
			} else {
				status("● 服务运行中")
				done = server.Done()
			}
			// Multiple clicks while restarting represent the same operation.
			select {
			case <-restart:
			default:
			}
		case err := <-done:
			done = nil
			logger.Error("desktop_http_failed", diagnostics.Fields(err)...)
			status("● 服务异常")
		}
	}
}

func runBrowser(ctx context.Context, requests <-chan *app.HTTP, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case server := <-requests:
			address := server.URL() // Recheck readiness when dequeuing, not only when clicked.
			if ctx.Err() != nil || address == "" {
				continue
			}
			if err := desktop.OpenBrowser(address); err != nil {
				logger.Error("browser_open_failed", diagnostics.Fields(err)...)
			}
		}
	}
}

func shutdownHTTP(server *app.HTTP, logger *slog.Logger) {
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		logger.Error("http_shutdown_failed", diagnostics.Fields(err)...)
	}
}
