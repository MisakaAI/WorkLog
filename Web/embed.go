//go:build desktop

// Package web embeds the complete Vite production build, including Monaco workers.
// Build Web/dist before compiling with -tags desktop (see scripts/build-windows.sh
// and scripts/build-macos.sh).
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var assets embed.FS

func Assets() fs.FS {
	// dist is guaranteed by the go:embed pattern at compile time.
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
