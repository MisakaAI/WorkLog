package integration

import (
	"fmt"
	"testing"

	"worklog/internal/domain"
	"worklog/internal/repository"
)

func TestHistoricalVersionsDoNotAmplifyDashboardSaves(t *testing.T) {
	f := setup(t, "sqlite")
	d, err := f.svc.SaveDashboard(ctx, owner, domain.Empty().Dashboard)
	must(t, err)
	tx, err := f.store.DB.BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO resource_versions(user_id,kind,id,version) VALUES ('owner',?,?,7)")
	must(t, err)
	for _, kind := range []string{"task", "todo"} {
		for i := range 1000 {
			_, err = stmt.ExecContext(ctx, kind, fmt.Sprintf("old-%d", i))
			must(t, err)
		}
	}
	must(t, stmt.Close())
	must(t, tx.Commit())
	for _, q := range []string{
		"CREATE TABLE version_writes (kind TEXT, id TEXT)",
		"CREATE TRIGGER count_version_insert AFTER INSERT ON resource_versions BEGIN INSERT INTO version_writes VALUES (new.kind,new.id); END",
		"CREATE TRIGGER count_version_update AFTER UPDATE ON resource_versions BEGIN INSERT INTO version_writes VALUES (new.kind,new.id); END",
	} {
		_, err = f.store.DB.ExecContext(ctx, q)
		must(t, err)
	}
	assertWrites := func(want int) {
		t.Helper()
		var count int
		must(t, f.store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM version_writes").Scan(&count))
		if count != want {
			t.Fatalf("resource version writes: got %d, want %d", count, want)
		}
	}
	// 墓碑不进入普通加载的数据量，也不因修改标题而执行任何版本写入。
	must(t, f.store.Within(ctx, owner.User, false, func(u repository.Unit) error {
		st, err := u.Load()
		if err == nil && (len(st.TaskVersions) != 0 || len(st.TodoVersions) != 0) {
			t.Fatal("Load scanned historical versions")
		}
		return err
	}))
	d.PersonalLabel = "changed"
	d, err = f.svc.SaveDashboard(ctx, owner, d)
	must(t, err)
	assertWrites(0)
	// 跨过批量查询边界，复用的任务与待办 ID 都必须延续墓碑版本。
	for i := range 450 {
		id := domain.StringID(fmt.Sprintf("old-%d", i))
		d.Tasks = append(d.Tasks, domain.Task{ID: id, Title: "reused", Status: "planned", Tag: d.Categories[0], Priority: "中", Color: "white"})
		d.Todos = append(d.Todos, domain.Todo{ID: id, Title: "reused"})
	}
	d, err = f.svc.SaveDashboard(ctx, owner, d)
	must(t, err)
	assertWrites(900)
	for _, table := range []string{"tasks", "todos"} {
		var count int
		must(t, f.store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE version=8").Scan(&count))
		if count != 450 {
			t.Fatalf("%s reused IDs reset versions: %d", table, count)
		}
	}
	d.PersonalLabel = "another title"
	d, err = f.svc.SaveDashboard(ctx, owner, d)
	must(t, err)
	assertWrites(900)
	for _, op := range []string{"update_task", "update_todo"} {
		_, err = f.svc.Execute(ctx, owner, op, raw(map[string]any{"id": "old-0", "expectedVersion": 7, "title": "stale"}))
		status(t, err, 409)
		_, err = f.svc.Execute(ctx, owner, op, raw(map[string]any{"id": "old-0", "expectedVersion": 8, "title": "updated"}))
		must(t, err)
	}
	assertWrites(902)
	// 快照删除不重写既有墓碑，其他历史记录仍保留。
	d, err = f.svc.Dashboard(ctx, owner)
	must(t, err)
	d.Tasks, d.Todos = []domain.Task{}, []domain.Todo{}
	_, err = f.svc.SaveDashboard(ctx, owner, d)
	must(t, err)
	assertWrites(902)
	var count int
	must(t, f.store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM resource_versions").Scan(&count))
	if count != 2000 {
		t.Fatalf("historical versions lost: %d", count)
	}
}
