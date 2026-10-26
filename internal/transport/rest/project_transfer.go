package rest

import (
	"io"
	"mime"
	"net/http"

	"worklog/internal/domain"
	"worklog/internal/transport/security"
)

func (s *Server) exportProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if len(q) > 1 || (len(q) == 1 && (len(q["projectId"]) != 1 || q.Get("projectId") == "")) {
		s.fail(w, r, domain.Bad("仅支持非空 projectId 参数"))
		return
	}
	data, err := s.Service.ExportProjects(r.Context(), security.Identity(r.Context()), q.Get("projectId"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="worklog-projects.zip"`)
	w.Write(data)
}

func (s *Server) importProjects(w http.ResponseWriter, r *http.Request) {
	if err := security.Identity(r.Context()).Check(true, false); err != nil {
		s.fail(w, r, err)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/zip" {
		s.fail(w, r, domain.Bad("Content-Type 必须为 application/zip"))
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, r, &domain.Error{Status: 413, Code: "PAYLOAD_TOO_LARGE", Message: "无法读取 ZIP 文件或文件过大"})
		return
	}
	out, err := s.Service.ImportProjects(r.Context(), security.Identity(r.Context()), data)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	respond(w, out)
}
