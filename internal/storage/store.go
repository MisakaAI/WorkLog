// Package storage 使用共用查询实现 SQLite 和 PostgreSQL 存储，
// 并将连接、事务和建表语法的数据库差异集中在本包处理。
package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"modernc.org/sqlite"
	"worklog/internal/diagnostics"
	"worklog/internal/domain"
	"worklog/internal/repository"
)

//go:embed migrations/*/*.sql
var migrations embed.FS

type Store struct {
	DB       *sql.DB
	postgres bool
	Path     string
}

func Open(ctx context.Context, driver, dsn string) (*Store, error) {
	s := &Store{postgres: driver == "postgres"}
	var dialect goose.Dialect
	switch driver {
	case "sqlite":
		if dsn == "" {
			dsn = "./data/worklog.db"
		}
		if strings.Contains(dsn, "?") || strings.HasPrefix(dsn, "file:") || dsn == ":memory:" {
			return nil, diagnostics.Safe("SQLite configuration must be a local file path")
		}
		p, err := filepath.Abs(dsn)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return nil, err
		}
		s.Path = p
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		u := sqliteFileURL(p)
		q := url.Values{}
		q.Add("_pragma", "busy_timeout(5000)")
		q.Add("_pragma", "foreign_keys(1)")
		q.Add("_pragma", "synchronous(NORMAL)")
		u.RawQuery = q.Encode()
		dsn = u.String()
		dialect = goose.DialectSQLite3
	case "postgres":
		driver = "pgx"
		dialect = goose.DialectPostgres
	default:
		return nil, diagnostics.Safe("unsupported database driver")
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	s.DB = db
	fail := func(e error) (*Store, error) { _ = db.Close(); return nil, e }
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		return fail(diagnostics.Wrap("ping_database", err))
	}
	folder := "sqlite"
	opts := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true), goose.WithSlog(slog.Default())}
	if s.postgres {
		folder = "postgres"
		l, e := lock.NewPostgresSessionLocker()
		if e != nil {
			return fail(e)
		}
		opts = append(opts, goose.WithSessionLocker(l))
	} else {
		var mode string
		if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			return fail(err)
		}
		if mode != "wal" {
			return fail(diagnostics.Safe("WAL mode unavailable; use a local filesystem"))
		}
	}
	sub, err := fs.Sub(migrations, "migrations/"+folder)
	if err != nil {
		return fail(err)
	}
	provider, err := goose.NewProvider(dialect, db, sub, opts...)
	if err != nil {
		return fail(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		return fail(diagnostics.Wrap("migrate_database", err))
	}
	return s, nil
}
func (s *Store) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }
func (s *Store) Close() error                   { return s.DB.Close() }
func (s *Store) query(q string) string {
	if !s.postgres {
		return q
	}
	n := 0
	return rebind(q, &n)
}

// rebind 仅适用于预先写好的 SQL：问号必须全部是参数占位符，
// 不能出现在字符串字面量中，也不能用作 PostgreSQL 运算符。
func rebind(q string, n *int) string {
	var b strings.Builder
	for _, c := range q {
		if c == '?' {
			*n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(*n))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	var se *sqlite.Error
	var pe *pgconn.PgError
	if errors.As(err, &se) && (se.Code()&255 == 5 || se.Code()&255 == 6) || errors.As(err, &pe) && (pe.Code == "40001" || pe.Code == "40P01" || pe.Code == "55P03") {
		return errors.Join(&domain.Error{Status: 503, Code: "DATABASE_BUSY", Message: "数据库繁忙，请重新读取后重试"}, err)
	}
	return err
}

// Within 在独占连接上执行完整事务；回调失败或提交未成功时都会尝试回滚。
// 同一工作台的写入由数据库串行化，保证多进程下版本校验与保存仍是原子操作。
func (s *Store) Within(ctx context.Context, user string, write bool, fn func(repository.Unit) error) (err error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return normalize(err)
	}
	defer conn.Close()
	// SQLite 提前取得写锁；PostgreSQL 写事务稍后锁定工作台行，
	// 只读事务则使用一致快照，避免多次查询读到不同批次的数据。
	begin := "BEGIN"
	if write && !s.postgres {
		begin = "BEGIN IMMEDIATE"
	}
	if write && s.postgres {
		begin = "BEGIN ISOLATION LEVEL READ COMMITTED READ WRITE"
	}
	if !write && s.postgres {
		begin = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"
	}
	if _, err = conn.ExecContext(ctx, begin); err != nil {
		return normalize(err)
	}
	committed := false
	defer func() {
		if !committed {
			// 请求取消后仍需清理事务，不能继续使用已取消的请求上下文。
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = conn.ExecContext(rollbackCtx, "ROLLBACK")
		}
		err = normalize(err)
	}()
	u := &unit{s: s, c: conn, ctx: ctx, user: user}
	if write {
		var result sql.Result
		result, err = u.exec("INSERT INTO workspaces(user_id) VALUES (?) ON CONFLICT(user_id) DO NOTHING", user)
		if err != nil {
			return err
		}
		affected, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if s.postgres {
			var ignored string
			if err = u.row("SELECT user_id FROM workspaces WHERE user_id=? FOR UPDATE", user).Scan(&ignored); err != nil {
				return err
			}
		}
		if affected == 1 {
			for i, c := range domain.Empty().Categories {
				if _, err = u.exec("INSERT INTO categories(user_id,name,name_key,position) VALUES (?,?,?,?)", user, c, strings.ToLower(c), i); err != nil {
					return err
				}
			}
		}
	}
	if err = fn(u); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	committed = err == nil
	return err
}

type unit struct {
	s    *Store
	c    *sql.Conn
	ctx  context.Context
	user string
}

func (u *unit) exec(q string, args ...any) (sql.Result, error) {
	return u.c.ExecContext(u.ctx, u.s.query(q), args...)
}
func (u *unit) row(q string, args ...any) *sql.Row {
	return u.c.QueryRowContext(u.ctx, u.s.query(q), args...)
}
func (u *unit) rows(q string, args ...any) (*sql.Rows, error) {
	return u.c.QueryContext(u.ctx, u.s.query(q), args...)
}
func collect[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (u *unit) Load() (*domain.State, error) {
	s := domain.Empty()
	err := u.row("SELECT revision,project_revision,category_revision,personal_label FROM workspaces WHERE user_id=?", u.user).Scan(&s.Revision, &s.ProjectRevision, &s.CategoryRevision, &s.PersonalLabel)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := u.rows("SELECT id,name,archived FROM projects WHERE user_id=? ORDER BY position", u.user)
	if err != nil {
		return nil, err
	}
	s.Projects, err = collect(rows, func(r *sql.Rows) (p domain.Project, e error) { e = r.Scan(&p.ID, &p.Name, &p.Archived); return })
	if err != nil {
		return nil, err
	}
	rows, err = u.rows("SELECT name FROM categories WHERE user_id=? ORDER BY position", u.user)
	if err != nil {
		return nil, err
	}
	s.Categories, err = collect(rows, func(r *sql.Rows) (n string, e error) { e = r.Scan(&n); return })
	if err != nil {
		return nil, err
	}
	rows, err = u.rows("SELECT id_json,title,description,COALESCE(project_id,''),status,tag,priority,start_date,due_date,color,version FROM tasks WHERE user_id=? ORDER BY position", u.user)
	if err != nil {
		return nil, err
	}
	s.Tasks, err = collect(rows, func(r *sql.Rows) (t domain.Task, e error) {
		var id string
		var v int64
		e = r.Scan(&id, &t.Title, &t.Description, &t.ProjectID, &t.Status, &t.Tag, &t.Priority, &t.StartDate, &t.Date, &t.Color, &v)
		t.ID = domain.ID(id)
		s.TaskVersions[t.ID.Key()] = v
		return
	})
	if err != nil {
		return nil, err
	}
	rows, err = u.rows("SELECT id_json,title,done,version FROM todos WHERE user_id=? ORDER BY position", u.user)
	if err != nil {
		return nil, err
	}
	s.Todos, err = collect(rows, func(r *sql.Rows) (t domain.Todo, e error) {
		var id string
		var v int64
		e = r.Scan(&id, &t.Title, &t.Done, &v)
		t.ID = domain.ID(id)
		s.TodoVersions[t.ID.Key()] = v
		return
	})
	if err != nil {
		return nil, err
	}
	// 删除资源后仍保留版本记录，避免同一 ID 被重新使用时接受旧版本请求。
	rows, err = u.rows("SELECT kind,id,version FROM resource_versions WHERE user_id=?", u.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		var version int64
		if err = rows.Scan(&kind, &id, &version); err != nil {
			return nil, err
		}
		if kind == "task" {
			s.TaskVersions[id] = version
		} else {
			s.TodoVersions[id] = version
		}
	}
	return s, rows.Err()
}
func (u *unit) Save(s *domain.State) error {
	for kind, versions := range map[string]map[string]int64{"task": s.TaskVersions, "todo": s.TodoVersions} {
		for id, version := range versions {
			if _, e := u.exec("INSERT INTO resource_versions(user_id,kind,id,version) VALUES (?,?,?,?) ON CONFLICT(user_id,kind,id) DO UPDATE SET version=excluded.version", u.user, kind, id, version); e != nil {
				return e
			}
		}
	}
	// 先删除依赖项，再在事务内重建已校验的数据关系。
	// 这样也能交换分类或项目名称，避免中间状态触发唯一性约束。
	for _, table := range []string{"tasks", "todos", "projects", "categories"} {
		if _, e := u.exec("DELETE FROM "+table+" WHERE user_id=?", u.user); e != nil {
			return e
		}
	}
	if _, e := u.exec("UPDATE workspaces SET revision=?,project_revision=?,category_revision=?,personal_label=? WHERE user_id=?", s.Revision, s.ProjectRevision, s.CategoryRevision, s.PersonalLabel, u.user); e != nil {
		return e
	}
	for i, p := range s.Projects {
		if _, e := u.exec("INSERT INTO projects(user_id,id,name,name_key,position,archived) VALUES (?,?,?,?,?,?)", u.user, p.ID, p.Name, strings.ToLower(p.Name), i, p.Archived); e != nil {
			return e
		}
	}
	for i, c := range s.Categories {
		if _, e := u.exec("INSERT INTO categories(user_id,name,name_key,position) VALUES (?,?,?,?)", u.user, c, strings.ToLower(c), i); e != nil {
			return e
		}
	}
	for i, t := range s.Tasks {
		var project any
		if t.ProjectID != "" {
			project = t.ProjectID
		}
		if _, e := u.exec("INSERT INTO tasks(user_id,id,id_json,title,description,project_id,status,tag,priority,start_date,due_date,color,version,position) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)", u.user, t.ID.Key(), string(t.ID), t.Title, t.Description, project, t.Status, t.Tag, t.Priority, t.StartDate, t.Date, t.Color, s.TaskVersions[t.ID.Key()], i); e != nil {
			return e
		}
	}
	for i, t := range s.Todos {
		if _, e := u.exec("INSERT INTO todos(user_id,id,id_json,title,done,version,position) VALUES (?,?,?,?,?,?,?)", u.user, t.ID.Key(), string(t.ID), t.Title, t.Done, s.TodoVersions[t.ID.Key()], i); e != nil {
			return e
		}
	}
	return nil
}
func (u *unit) Worklog(date string) (domain.Worklog, error) {
	w := domain.EmptyWorklog(date)
	var content string
	var updated string
	err := u.row("SELECT content,version,updated_at FROM worklogs WHERE user_id=? AND date=?", u.user, date).Scan(&content, &w.Version, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return w, nil
	}
	if err != nil {
		return w, err
	}
	w.Content = json.RawMessage(content)
	w.UpdatedAt = &updated
	return w, nil
}
func (u *unit) SaveWorklog(w domain.Worklog, has bool) error {
	_, err := u.exec("INSERT INTO worklogs(user_id,date,content,has_content,version,updated_at) VALUES (?,?,?,?,?,?) ON CONFLICT(user_id,date) DO UPDATE SET content=excluded.content,has_content=excluded.has_content,version=excluded.version,updated_at=excluded.updated_at", u.user, w.Date, string(w.Content), has, w.Version, *w.UpdatedAt)
	return err
}
func (u *unit) Dates(start, end string) ([]string, error) {
	q := "SELECT date FROM worklogs WHERE user_id=? AND has_content=?"
	args := []any{u.user, true}
	if start != "" {
		q += " AND date>=?"
		args = append(args, start)
	}
	if end != "" {
		q += " AND date<=?"
		args = append(args, end)
	}
	q += " ORDER BY date"
	r, e := u.rows(q, args...)
	if e != nil {
		return nil, e
	}
	return collect(r, func(r *sql.Rows) (d string, e error) { e = r.Scan(&d); return })
}
func (u *unit) Idempotency(key string) (string, json.RawMessage, error) {
	var fingerprint, response string
	err := u.row("SELECT fingerprint,response FROM idempotency WHERE user_id=? AND key=?", u.user, key).Scan(&fingerprint, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	return fingerprint, json.RawMessage(response), err
}
func (u *unit) Remember(key, fingerprint string, response json.RawMessage) error {
	_, err := u.exec("INSERT INTO idempotency(user_id,key,fingerprint,response) VALUES (?,?,?,?)", u.user, key, fingerprint, string(response))
	return err
}

// Backup 使用 SQLite 在线一致性快照备份；不能直接复制运行中的 .db 文件，
// 否则会遗漏 WAL 中已经提交的数据。
func (s *Store) Backup(ctx context.Context, destination string) error {
	if s.postgres {
		return diagnostics.Safe("use pg_dump for PostgreSQL")
	}
	p, e := filepath.Abs(destination)
	if e != nil {
		return e
	}
	if p == s.Path {
		return diagnostics.Safe("backup cannot overwrite database")
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	if _, e = os.Stat(p); !errors.Is(e, os.ErrNotExist) {
		return diagnostics.Safe("backup destination must not exist")
	}
	// VACUUM INTO 允许写入已有空文件。先以 0600 权限独占创建目标，
	// 确保即使目录被共享，备份内容在写入前也已受到权限保护。
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		_ = os.Remove(p)
		return e
	}
	if _, e = s.DB.ExecContext(ctx, "VACUUM INTO ?", p); e != nil {
		_ = os.Remove(p)
		return e
	}
	f, e = os.OpenFile(p, os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// sqliteFileURL converts an absolute native path to a SQLite file URI. In
// particular C:\\... must become file:///C:/..., not a URI with C: as its host.
func sqliteFileURL(path string) url.URL {
	path = filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return url.URL{Scheme: "file", Path: path}
}

func VerifySQLite(ctx context.Context, path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := sqliteFileURL(absolute)
	q := url.Values{"mode": {"ro"}, "_pragma": {"foreign_keys(1)"}}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return e
	}
	defer db.Close()
	var result string
	if e = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return diagnostics.Safe("integrity check failed")
	}
	rows, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		return diagnostics.Safe("foreign key check failed")
	}
	return rows.Err()
}
