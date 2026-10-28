package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "time/tzdata"
	"worklog/internal/app"
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
	srv := app.NewHTTP(c.Addr, rest.New(svc, c, logger), logger)
	if err = srv.Start(); err != nil {
		return diagnostics.Wrap("start_http", err)
	}
	logger.Info("server_started", "url", srv.URL(), "database", c.Driver, "business_timezone", c.Timezone, "local_mode", c.Local, "mcp_destructive_enabled", c.MCPDestructive)
	select {
	case err = <-srv.Done():
	case <-ctx.Done():
	}
	// Stop and drain every request before the deferred database Close, including
	// when Serve failed unexpectedly. The shutdown context is independent of ctx.
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return errors.Join(err, srv.Shutdown(shutdown))
}

// parseArgs 要求全局选项位于子命令之前，例如 worklog --conf=/etc/worklog backup out.db。
func parseArgs(args []string) (string, []string, error) {
	flags := flag.NewFlagSet("worklog", flag.ContinueOnError)
	conf := flags.String("conf", "", "JSON configuration file or directory containing config.json (otherwise uses built-in defaults)")
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
	return storage.RestoreSQLite(ctx, source, destination)
}
