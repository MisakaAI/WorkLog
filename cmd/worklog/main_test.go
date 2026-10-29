package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"worklog/internal/domain"
	"worklog/internal/service"
	"worklog/internal/storage"
)

func TestRestoreCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, e := storage.Open(ctx, "sqlite", filepath.Join(dir, "source.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	id := domain.Identity{User: "owner", Write: true, Destructive: true}
	svc := service.New(store, time.UTC)
	_, e = svc.Execute(ctx, id, "create_task", []byte(`{"title":"restore me","idempotencyKey":"r"}`))
	if e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(dir, "backup.db")
	if e = store.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(dir, "restored.db")
	if e = restore(ctx, backup, target); e != nil {
		t.Fatal(e)
	}
	if e = restoreWithIO(ctx, backup, target, strings.NewReader("\n"), io.Discard, time.Now); e == nil {
		t.Fatal("restore overwrote existing database without confirmation")
	}
	restored, e := storage.Open(ctx, "sqlite", target)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	d, e := service.New(restored, time.UTC).Dashboard(ctx, id)
	if e != nil || len(d.Tasks) != 1 || d.Tasks[0].Title != "restore me" {
		t.Fatal(d, e)
	}
	bad := filepath.Join(dir, "bad.db")
	if e = os.WriteFile(bad, []byte("not sqlite"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = restore(ctx, bad, filepath.Join(dir, "bad-target.db")); e == nil {
		t.Fatal("corrupt backup accepted")
	}
}
