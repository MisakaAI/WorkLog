//go:build desktop && (windows || (darwin && cgo))

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"worklog/internal/config"
	"worklog/internal/storage"
)

// An unpredictable suffix avoids collisions even for multiple exports per second.
// Backup and RestoreSQLite still exclusively create their destinations.
func newDatabasePath(directory, prefix string) string {
	return filepath.Join(directory, fmt.Sprintf("%s-%s-%s.db", prefix, time.Now().Format("20060102-150405"), uuid.NewString()))
}

// The caller must drain HTTP and close its store first. Never restore over the
// original database: keep it for rollback and switch only after validation.
func importDatabase(ctx context.Context, c config.Config, conf, source string) (string, string, error) {
	if c.DSN == "" {
		c.DSN = "./data/worklog.db"
	}
	destination, err := filepath.Abs(newDatabasePath(filepath.Dir(c.DSN), "worklog-restored"))
	if err != nil {
		return "", "", err
	}
	if err = storage.RestoreSQLite(ctx, source, destination); err != nil {
		return "", "", err
	}
	// Validate application schema/migrations before changing the active config.
	store, err := storage.Open(ctx, "sqlite", destination)
	if err != nil {
		return "", "", err
	}
	if err = store.Close(); err != nil {
		return "", "", err
	}
	if err = storage.VerifySQLite(ctx, destination); err != nil {
		return "", "", err
	}
	path, err := switchDatabaseConfig(conf, destination)
	return path, destination, err
}

// Preserve the original JSON settings (including omitted defaults). Write next
// to the original file, flush, then atomically replace it on the same volume.
func switchDatabaseConfig(conf, destination string) (string, error) {
	settings := make(map[string]json.RawMessage)
	defaultConfig := conf == ""
	if defaultConfig {
		conf = "config.json"
		// With built-in defaults no config exists. Do not overwrite a file that
		// appeared since startup; read and preserve it if present.
		if _, err := os.Stat(conf); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	info, err := os.Stat(conf)
	if err == nil && info.IsDir() {
		conf = filepath.Join(conf, "config.json")
	}
	data, err := os.ReadFile(conf)
	if err == nil {
		if err = json.Unmarshal(data, &settings); err != nil {
			return "", err
		}
		if settings == nil {
			return "", fmt.Errorf("配置必须是 JSON 对象")
		}
	} else if !defaultConfig || !os.IsNotExist(err) {
		return "", err
	}
	settings["db_dsn"], err = json.Marshal(destination)
	if err != nil {
		return "", err
	}
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := createPrivateConfigTemp(filepath.Dir(conf))
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = replaceConfigFile(f.Name(), conf); err != nil {
		return "", err
	}
	return conf, nil
}
