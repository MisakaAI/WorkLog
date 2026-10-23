package integration

import (
	"testing"

	"worklog/internal/service"
)

func TestPostgresDateStyleIndependence(t *testing.T) {
	if postgresAbsent() {
		t.Skip("set WORKLOG_TEST_POSTGRES_DSN")
	}
	f := setup(t, "postgres")
	f.store.DB.SetMaxOpenConns(1)
	_, e := f.store.DB.ExecContext(ctx, "SET DateStyle = 'SQL, DMY'")
	must(t, e)
	invoke(t, f, owner, "create_task", service.CreateTask{Title: "date style", StartDate: "2026-10-20", Date: "2026-10-21", IdempotencyKey: "date-style"})
	_, e = f.svc.SaveWorklog(ctx, owner, "2026-10-21", []byte(`{"type":"doc","content":[{"type":"horizontalRule"}]}`), 0)
	must(t, e)
	dates, e := f.svc.Dates(ctx, owner, "2026-10-21", "2026-10-21")
	must(t, e)
	if len(dates) != 1 || dates[0] != "2026-10-21" {
		t.Fatal(dates)
	}
}
