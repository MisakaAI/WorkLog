//go:build desktop && (windows || (darwin && cgo))

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"worklog/internal/config"
	"worklog/internal/storage"
)

func TestSwitchDatabaseConfigPreservesSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := `{"db_dsn":"old.db","basic_password":"private-password","allowed_origins":[],"db_driver":"sqlite"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(dir, "恢复.db")
	if _, err := switchDatabaseConfig(dir, next); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var dsn string
	if err := json.Unmarshal(got["db_dsn"], &dsn); err != nil || dsn != next {
		t.Fatalf("dsn = %q, error %v", dsn, err)
	}
	if string(got["basic_password"]) != `"private-password"` || string(got["allowed_origins"]) != "[]" || len(got) != 4 {
		t.Fatalf("settings changed: %s", data)
	}
}

func TestSwitchDatabaseConfigMissingExplicitFile(t *testing.T) {
	if _, err := switchDatabaseConfig(filepath.Join(t.TempDir(), "missing.json"), "new.db"); err == nil {
		t.Fatal("missing explicit config must not be silently replaced")
	}
}

func TestImportDatabasePreservesOriginal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	original := filepath.Join(dir, "original.db")
	store, err := storage.Open(ctx, "sqlite", original)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err = store.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]string{"db_dsn": original})
	if err = os.WriteFile(conf, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, restored, err := importDatabase(ctx, config.Config{DSN: original}, conf, backup)
	if err != nil {
		t.Fatal(err)
	}
	if restored == original || restored == backup {
		t.Fatalf("restored over existing database: %s", restored)
	}
	after, err := os.ReadFile(original)
	if err != nil || string(before) != string(after) {
		t.Fatalf("original database changed: %v", err)
	}
	if err := storage.VerifySQLite(ctx, restored); err != nil {
		t.Fatal(err)
	}
	configBefore, _ := os.ReadFile(conf)
	if _, _, err := importDatabase(ctx, config.Config{DSN: original}, conf, filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("missing source accepted")
	}
	configAfter, _ := os.ReadFile(conf)
	if string(configBefore) != string(configAfter) {
		t.Fatal("failed import modified configuration")
	}
}
