package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "time/tzdata"
	"worklog/internal/config"
	"worklog/internal/diagnostics"
	"worklog/internal/domain"
	"worklog/internal/service"
	"worklog/internal/storage"
	mcptransport "worklog/internal/transport/mcp"
	"worklog/internal/transport/rest"
	"worklog/internal/transport/security"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("startup_failed", diagnostics.Fields(err)...)
		os.Exit(1)
	}
}

// run 共用配置与数据库初始化流程，再按命令进入 HTTP、stdio 或数据维护模式。
func run(logger *slog.Logger) error {
	conf, args, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}
	var c config.Config
	if conf != "" {
		c, err = config.LoadFile(conf)
	} else {
		c, err = config.Load()
	}
	if err != nil {
		return diagnostics.Wrap("load_configuration", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if command == "restore" {
		if len(args) != 2 || c.Driver != "sqlite" {
			return diagnostics.Safe("usage: worklog restore backup.db (SQLite only; destination must not exist)")
		}
		return diagnostics.Wrap("restore_database", restore(ctx, args[1], c.DSN))
	}
	if command != "serve" && command != "stdio" && command != "migrate" && command != "backup" {
		return diagnostics.Safe("commands: serve | stdio | migrate | backup PATH | restore PATH")
	}
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	store, err := storage.Open(startup, c.Driver, c.DSN)
	cancel()
	if err != nil {
		return diagnostics.Wrap("initialize_"+c.Driver+"_database", err)
	}
	defer store.Close()
	if command == "migrate" {
		logger.Info("migrations_complete")
		return nil
	}
	if command == "backup" {
		if len(args) != 2 {
			return diagnostics.Safe("usage: worklog backup new-backup.db")
		}
		if err = store.Backup(ctx, args[1]); err != nil {
			return diagnostics.Wrap("backup_database", err)
		}
		return diagnostics.Wrap("verify_backup", storage.VerifySQLite(ctx, args[1]))
	}
	loc, _ := time.LoadLocation(c.Timezone)
	svc := service.New(store, loc)
	if command == "stdio" {
		id := domain.Identity{User: c.User, Write: !c.StdioReadOnly, Destructive: c.MCPDestructive}
		server := mcptransport.New(svc, id, logger, security.NewLimiter())
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	srv := rest.HTTPServer(c.Addr, rest.New(svc, c, logger))
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	logger.Info("server_started", "address", c.Addr, "database", c.Driver, "business_timezone", c.Timezone, "local_mode", c.Local, "mcp_destructive_enabled", c.MCPDestructive)
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// 收到退出信号后使用独立上下文，给正在处理的请求留出完成时间。
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// parseArgs 要求全局选项位于子命令之前，例如 worklog --conf=/etc/worklog backup out.db。
func parseArgs(args []string) (string, []string, error) {
	flags := flag.NewFlagSet("worklog", flag.ContinueOnError)
	conf := flags.String("conf", "", "JSON configuration file or directory containing config.json (ignores WORKLOG_* environment variables)")
	flags.Usage = func() {
		_, _ = io.WriteString(flags.Output(), "Usage: worklog [--conf PATH] [serve | stdio | migrate | backup PATH | restore PATH]\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", nil, err
		}
		return "", nil, diagnostics.Safe("invalid command-line options; use worklog --help")
	}
	var empty bool
	flags.Visit(func(f *flag.Flag) { empty = f.Name == "conf" && f.Value.String() == "" })
	if empty {
		return "", nil, diagnostics.Safe("--conf must not be empty")
	}
	return *conf, flags.Args(), nil
}

func restore(ctx context.Context, source, destination string) error {
	src, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	dst, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if src == dst {
		return diagnostics.Safe("source and destination must differ")
	}
	if info, e := os.Stat(src + "-wal"); e == nil && info.Size() > 0 {
		return diagnostics.Safe("restore requires a standalone backup, not a live WAL database")
	}
	if err = storage.VerifySQLite(ctx, src); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	// 独占创建目标，避免覆盖现有数据。恢复前必须停止所有写入进程，
	// 并将旧数据库及对应的 WAL/SHM 文件一并移走。
	for _, p := range []string{dst, dst + "-wal", dst + "-shm"} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			return diagnostics.Safe("destination database/WAL/SHM must not exist")
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = storage.VerifySQLite(ctx, dst); err != nil {
		return err
	}
	ok = true
	return nil
}
