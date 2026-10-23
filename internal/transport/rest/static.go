package rest

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"worklog/internal/domain"
)

func staticName(urlPath string) (string, bool) {
	if strings.Contains(urlPath, "\\") || !strings.HasPrefix(urlPath, "/") {
		return "", false
	}
	for _, part := range strings.Split(urlPath, "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	name := strings.TrimPrefix(urlPath, "/")
	if name == "" {
		return "index.html", true
	}
	switch name {
	case "index.html", "favicon.svg", "favicon.ico", "logo.png":
		return name, true
	}
	if !strings.HasPrefix(name, "assets/") {
		return "", false
	}
	// 只提供允许的构建产物，不开放任意数据库、备份或源码文件。
	switch path.Ext(name) {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".svg", ".png", ".webp", ".gif", ".ico":
		return name, true
	}
	return "", false
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	name, ok := staticName(r.URL.Path)
	if !ok {
		s.fail(w, r, domain.NotFound())
		return
	}
	// os.Root 防止通过静态目录内的符号链接访问目录之外的文件。
	// 每次请求重新打开目录，确保替换 dist 后不会继续引用旧目录。
	root, err := os.OpenRoot(s.Config.StaticDir)
	if err != nil {
		s.fail(w, r, domain.NotFound())
		return
	}
	defer root.Close()
	info, err := fs.Stat(root.FS(), name)
	if err != nil || !info.Mode().IsRegular() {
		s.fail(w, r, domain.NotFound())
		return
	}
	if strings.HasPrefix(name, "assets/") {
		// 仅允许私有缓存，避免共享缓存绕过身份认证。
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, root.FS(), name)
}
