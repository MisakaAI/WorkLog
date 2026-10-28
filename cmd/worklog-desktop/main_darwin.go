//go:build darwin && desktop && cgo

// Worklog runs AppKit, the Go service and embedded Vue assets in one process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
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

// AppKit's event loop must remain on the process's original main thread.
func init() { runtime.LockOSThread() }

func main() {
	if err := run(); err != nil {
		desktop.ShowError(err.Error())
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("Worklog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	conf := flags.String("conf", "", "configuration JSON file or directory")
	background := flags.Bool("autostart", false, "start without automatically opening the browser")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("用法: Worklog [--conf 配置文件或目录]")
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
	var err error
	if *conf != "" {
		*conf, err = filepath.Abs(*conf)
		if err != nil {
			return err
		}
	}
	root, err := os.UserConfigDir() // ~/Library/Application Support on macOS.
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
	if err = desktop.SecureDataRoot(root); err != nil {
		return err
	}
	// Acquire ownership before opening configuration, database, tokens or logs.
	instance, primary, err := desktop.AcquireInstance(root, !*background)
	if err != nil {
		return fmt.Errorf("单实例初始化失败: %w", err)
	}
	if !primary {
		return nil
	}
	defer instance.Close()
	if err = desktop.SecureDataDir(root); err != nil {
		return err
	}
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	open, restart := make(chan struct{}, 1), make(chan struct{}, 1)
	backups := make(chan desktop.BackupRequest, 1)
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
	browserRequests := make(chan *app.HTTP, 1)
	// A blocked external browser must not delay restart or database cleanup.
	go runBrowser(ctx, browserRequests, logger)
	workerDone := make(chan struct{})
	started := false // Called only on the AppKit thread.
	desktop.RunMenuBar(func(menu *desktop.MenuBar) {
		started = true
		go func() {
			defer close(workerDone)
			defer menu.Close()
			runService(ctx, *conf, logger, menu, open, restart, browserRequests, web.Assets(), serviceOptions{background: *background, backups: backups, notify: menu.Notify})
		}()
	}, func() { enqueue(open) }, func() { enqueue(restart) }, cancel, autostart, func(request desktop.BackupRequest) {
		select {
		case backups <- request:
		default:
		}
	})
	cancel()
	if started {
		<-workerDone
	}
	<-watchDone
	logger.Info("desktop_stopped")
	return nil
}

func openLog(root string) (*os.File, error) {
	path := filepath.Join(root, "worklog.log")
	if info, err := os.Stat(path); err == nil && info.Size() >= 5<<20 {
		if err = os.Remove(path + ".1"); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err = os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

type statusDisplay interface{ SetStatus(string) }

type serviceOptions struct {
	background bool
	backups    <-chan desktop.BackupRequest
	notify     func(bool, string)
}

// Initialization, restart and cleanup are serialized. Like the Windows host,
// HTTP restarts retain the handler, database, authentication and bound port.
func runService(ctx context.Context, conf string, logger *slog.Logger, menu statusDisplay, open, restart <-chan struct{}, browserRequests chan<- *app.HTTP, assets fs.FS, options serviceOptions) {
	var store *storage.Store
	var server *app.HTTP
	var activeConfig, nextStartConfig *config.Config
	notify := func(failed bool, message string) {
		if options.notify != nil {
			options.notify(failed, message)
		}
	}
	status := func(label string) {
		menu.SetStatus(label)
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
			var c config.Config
			var err error
			if nextStartConfig != nil {
				c = *nextStartConfig
			} else {
				c, err = config.LoadDesktop(conf)
			}
			if err != nil {
				return diagnostics.Wrap("load_configuration", err)
			}
			startup, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			store, err = storage.Open(startup, c.Driver, c.DSN)
			if err != nil {
				return diagnostics.Wrap("initialize_database", err)
			}
			activeConfig, nextStartConfig = &c, nil
			loc, _ := time.LoadLocation(c.Timezone)
			server = app.NewHTTP(c.Addr, rest.NewWithStaticFS(service.New(store, loc), c, logger, assets), logger)
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
		default:
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
		autoOpen := !options.background
		select {
		case <-open:
			autoOpen = true
		default:
		} // Coalesce startup/reopen notifications.
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
			select {
			case <-restart:
			default:
			}
		case request := <-options.backups:
			if ctx.Err() != nil {
				return
			}
			var c config.Config
			var err error
			if activeConfig != nil {
				c = *activeConfig
			} else {
				c, err = config.LoadDesktop(conf)
			}
			if err == nil && request.Import && activeConfig != nil {
				var diskConfig config.Config
				diskConfig, err = config.LoadDesktop(conf)
				if err == nil && !reflect.DeepEqual(diskConfig, c) {
					err = errors.New("配置文件已修改，请完整退出并重新启动 Worklog 后再导入备份")
				}
			}
			if err == nil && c.Driver != "sqlite" {
				err = errors.New("菜单导入／导出仅支持 SQLite；PostgreSQL 请使用 pg_dump / pg_restore")
			}
			if err != nil {
				notify(true, "无法执行备份操作。\n"+err.Error())
				continue
			}
			if !request.Import {
				if store == nil {
					notify(true, "数据库未就绪，请先重启服务。")
					continue
				}
				status("● 正在导出备份")
				path := newDatabasePath(request.Path, "worklog-backup")
				err = store.Backup(ctx, path)
				if err == nil {
					err = storage.VerifySQLite(ctx, path)
				}
				if err != nil {
					logger.Error("desktop_backup_failed", diagnostics.Fields(err)...)
					notify(true, "导出备份失败。\n"+err.Error())
				} else {
					notify(false, "备份已导出：\n"+path+"\n备份包含私人数据，请妥善保管。")
				}
			} else {
				status("● 正在导入备份")
				// Recover with the active configuration if validation/switch fails.
				nextStartConfig = &c
				done = nil
				if server != nil {
					shutdownHTTP(server, logger)
					server = nil
				}
				if store != nil {
					err = store.Close()
					store = nil
				}
				var nextConf, restored string
				if err == nil {
					nextConf, restored, err = importDatabase(ctx, c, conf, request.Path)
				}
				if err != nil {
					logger.Error("desktop_restore_failed", diagnostics.Fields(err)...)
					notify(true, "导入失败，未切换数据库；将尝试重新启动原服务。\n"+err.Error())
				} else {
					conf, nextStartConfig = nextConf, nil
					notify(false, "备份已导入，原数据库已保留。\n新数据库："+restored+"\n请重新打开 Worklog，检查任务与日志。")
				}
				if err := start(); err != nil {
					logger.Error("desktop_restore_restart_failed", diagnostics.Fields(err)...)
					status("● 服务异常")
					notify(true, "服务启动失败，可从菜单重试。\n"+err.Error())
					continue
				}
				done = server.Done()
			}
			if server != nil && server.URL() != "" {
				status("● 服务运行中")
			} else {
				status("● 服务异常")
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
			address := server.URL()
			if ctx.Err() != nil || address == "" {
				continue
			}
			if err := desktop.OpenBrowser(address); err != nil {
				logger.Error("browser_open_failed", diagnostics.Fields(err)...)
			} else {
				logger.Info("browser_opened", "url", address)
			}
		}
	}
}

func shutdownHTTP(server *app.HTTP, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http_shutdown_failed", diagnostics.Fields(err)...)
	}
}
