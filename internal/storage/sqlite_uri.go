package storage

import (
	"net/url"
	"path/filepath"
	"strings"
)

// sqliteFileURL converts an absolute OS path to a SQLite file URI. Windows
// drive paths must use forward slashes and start with / (file:///C:/...), or
// net/url treats the drive as a URI authority, which SQLite rejects at open.
// Use URL escaping so #, %, spaces and non-ASCII filenames remain file paths.
func sqliteFileURL(path string) url.URL {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return url.URL{Scheme: "file", Path: path}
}
