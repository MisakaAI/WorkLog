package integration

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"worklog/internal/domain"
	"worklog/internal/service"
	"worklog/internal/storage"
)

func postgresAbsent() bool { return os.Getenv("WORKLOG_TEST_POSTGRES_DSN") == "" }

func TestSQLiteBusyIsExplicit(t *testing.T) {
	f := setup(t, "sqlite")
	invoke(t, f, owner, "create_todo", service.CreateTodo{Title: "init", IdempotencyKey: "init"})
	second, e := storage.Open(ctx, "sqlite", f.dsn)
	must(t, e)
	defer second.Close()
	second.DB.SetMaxOpenConns(1)
	_, e = second.DB.ExecContext(ctx, "PRAGMA busy_timeout=30")
	must(t, e)
	conn, e := f.store.DB.Conn(ctx)
	must(t, e)
	defer conn.Close()
	_, e = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	must(t, e)
	defer conn.ExecContext(ctx, "ROLLBACK")
	svc := service.New(second, f.svc.Location)
	_, e = svc.Execute(ctx, owner, "create_todo", raw(service.CreateTodo{Title: "blocked", IdempotencyKey: "blocked"}))
	status(t, e, 503)
	if domain.Public(e).Code != "DATABASE_BUSY" {
		t.Fatal(e)
	}
}
func TestSQLiteWALParallelReadersWriters(t *testing.T) {
	f := setup(t, "sqlite")
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for i := range 40 {
		wg.Add(2)
		go func() { defer wg.Done(); _, e := f.svc.Dashboard(ctx, owner); errs <- e }()
		go func() {
			defer wg.Done()
			_, e := f.svc.Execute(ctx, owner, "create_todo", raw(service.CreateTodo{Title: "parallel", IdempotencyKey: time.Unix(int64(i), 0).String()}))
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	d, e := f.svc.Dashboard(ctx, owner)
	must(t, e)
	if len(d.Todos) != 40 || d.Revision != 40 {
		t.Fatal(len(d.Todos), d.Revision)
	}
}
func TestDatabaseRejectsInvalidGraph(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && testing.Short() {
				t.Skip("short mode")
			}
			if driver == "postgres" && postgresAbsent() {
				t.Skip("set WORKLOG_TEST_POSTGRES_DSN")
			}
			f := setup(t, driver)
			_, e := f.svc.SaveDashboard(ctx, owner, snapshotFixture())
			must(t, e)
			// SQL-level FKs/enums/unique/checks independently enforce integrity.
			tests := []string{"DELETE FROM projects WHERE user_id='owner' AND id='p'", "DELETE FROM categories WHERE user_id='owner' AND name='A'", "UPDATE tasks SET status='invalid' WHERE user_id='owner'", "UPDATE tasks SET version=0 WHERE user_id='owner'", "UPDATE tasks SET due_date='2026-02-30' WHERE user_id='owner'", "UPDATE tasks SET due_date='2020-01-01' WHERE user_id='owner'", "UPDATE workspaces SET revision=-1 WHERE user_id='owner'", "INSERT INTO projects(user_id,id,name,name_key,position) VALUES ('owner','p2','Another','project',2)"}
			for _, q := range tests {
				if _, e = f.store.DB.ExecContext(ctx, q); e == nil {
					t.Errorf("database accepted: %s", q)
				}
			}
		})
	}
}
func TestBackupWhileWriting(t *testing.T) {
	f := setup(t, "sqlite")
	done := make(chan error, 1)
	go func() {
		for i := range 12 {
			_, e := f.svc.Execute(ctx, owner, "create_todo", raw(service.CreateTodo{Title: "consistent", IdempotencyKey: time.Unix(int64(i), 0).String()}))
			if e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	path := filepath.Join(t.TempDir(), "consistent.db")
	must(t, f.store.Backup(ctx, path))
	must(t, <-done)
	must(t, storage.VerifySQLite(ctx, path))
	restored, e := storage.Open(ctx, "sqlite", path)
	must(t, e)
	defer restored.Close()
	d, e := service.New(restored, f.svc.Location).Dashboard(ctx, owner)
	must(t, e)
	if d.Revision != int64(len(d.Todos)) {
		t.Fatal("inconsistent snapshot", d.Revision, len(d.Todos))
	}
}
