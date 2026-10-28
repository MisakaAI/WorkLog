//go:build darwin && desktop && cgo

package main

import (
	"os"

	"worklog/internal/desktop"
)

func createPrivateConfigTemp(dir string) (*os.File, error) {
	file, err := os.CreateTemp(dir, ".worklog-config-*.json")
	if err != nil {
		return nil, err
	}
	if err := desktop.CheckPrivateFile(file); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func replaceConfigFile(source, destination string) error {
	return os.Rename(source, destination)
}
