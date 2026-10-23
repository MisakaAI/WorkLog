package config

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"worklog/internal/diagnostics"
)

func readFile(path string, c *Config) error {
	info, err := os.Stat(path)
	if err != nil {
		return diagnostics.Wrap("open_configuration_file", err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "config.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return diagnostics.Wrap("open_configuration_file", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	target := c
	if err := decoder.Decode(&target); err != nil || target == nil {
		// JSON 解析错误可能包含输入值或密钥，不能直接写入日志。
		return diagnostics.Safe("invalid configuration JSON: check field names and value types")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return diagnostics.Safe("configuration file must contain exactly one JSON object")
	}
	c.configFile = path
	return nil
}
