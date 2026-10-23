package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"worklog/internal/config"
	"worklog/internal/diagnostics"
	"worklog/internal/domain"
	"worklog/internal/service"
	mcptransport "worklog/internal/transport/mcp"
	"worklog/internal/transport/security"
)

type Server struct {
	Service     *service.Service
	Config      config.Config
	Logger      *slog.Logger
	limiter     *security.Limiter
	healthSlots chan struct{}
}
type requestKey struct{}

func New(svc *service.Service, c config.Config, logger *slog.Logger) http.Handler {
	s := &Server{Service: svc, Config: c, Logger: logger, limiter: security.NewLimiter(), healthSlots: make(chan struct{}, 4)}
	r := chi.NewRouter()
	r.Use(middleware.GetHead)
	r.Use(s.middleware)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.healthSlots <- struct{}{}:
			defer func() { <-s.healthSlots }()
		default:
			s.fail(w, r, &domain.Error{Status: 503, Code: "UNAVAILABLE", Message: "健康检查繁忙"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e := svc.Repo.Ping(ctx); e != nil {
			s.fail(w, r, errors.Join(&domain.Error{Status: 503, Code: "UNAVAILABLE", Message: "数据库暂不可用"}, diagnostics.Wrap("health_ping", e)))
			return
		}
		respond(w, map[string]bool{"ok": true})
	})
	r.Route("/api", func(r chi.Router) {
		r.Get("/dashboard", s.dashboard)
		r.Put("/dashboard", s.saveDashboard)
		r.Get("/projects", s.operation("list_projects"))
		r.Put("/projects", s.operation("replace_projects"))
		r.Get("/categories", s.operation("list_categories"))
		r.Put("/categories", s.operation("manage_categories"))
		r.Get("/tasks", s.operation("list_tasks"))
		r.Post("/tasks", s.operation("create_task"))
		r.Get("/tasks/{id}", s.operation("get_task"))
		r.Patch("/tasks/{id}", s.operation("update_task"))
		r.Delete("/tasks/{id}", s.operation("delete_task"))
		r.Get("/todos", s.operation("list_todos"))
		r.Post("/todos", s.operation("create_todo"))
		r.Patch("/todos/{id}", s.operation("update_todo"))
		r.Delete("/todos/{id}", s.operation("delete_todo"))
		r.Get("/today", s.worklog)
		r.Get("/worklog/dates", s.dates)
		r.Get("/worklog/{date}", s.worklog)
		r.Put("/worklog/{date}", s.saveWorklog)
	})
	mh := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return mcptransport.New(svc, security.Identity(r.Context()), logger, nil)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: logger, MaxRequestBodyBytes: 2 << 20, PropagateRequestCancellation: true,
		// 每个请求均由 Trusted 中间件严格检查 Host/Origin 白名单。
		// 关闭 SDK 仅允许本机域名的额外限制，避免误拒经本机 HTTPS 反向代理
		// 转发、且已在白名单中的公网域名。
		DisableLocalhostProtection: true})
	r.Handle("/mcp", mh)
	if c.StaticDir != "" {
		r.Get("/config.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = fmt.Fprintf(w, "window.WORKLOG_CONFIG = { apiBaseUrl: '/api', businessTimezone: %q };", c.Timezone)
		})
		r.Get("/*", s.static)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, domain.NotFound()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		s.fail(w, r, &domain.Error{Status: 405, Code: "METHOD_NOT_ALLOWED", Message: "不支持此 HTTP 方法"})
	})
	return r
}
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := "req_" + uuid.NewString()
		r.Header.Set("X-Request-ID", requestID) // 覆盖不可信的传入值，同时用于关联 MCP 审计日志
		r = r.WithContext(context.WithValue(r.Context(), requestKey{}, requestID))
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			if p := recover(); p != nil {
				// panic 值可能含凭证或正文，只记录类型和堆栈。
				// 响应已经开始发送时，不再追加 JSON 错误，以免破坏响应格式。
				s.Logger.Error("request_panic", "requestId", requestID, "panic_type", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
				if ww.Status() == 0 {
					s.fail(ww, r, &domain.Error{Status: 500, Code: "INTERNAL_ERROR", Message: "服务暂时不可用"})
				}
			}
			s.Logger.Info("http_request", "requestId", requestID, "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds())
		}()
		if e := security.Trusted(s.Config, r); e != nil {
			s.fail(ww, r, e)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, MCP-Protocol-Version")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, Idempotency-Key, MCP-Protocol-Version, MCP-Session-Id, Last-Event-ID")
			ww.WriteHeader(200)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && len(r.URL.Query()) != 0 {
			s.fail(ww, r, domain.Bad("写接口不接受查询参数"))
			return
		}
		if r.URL.Path != "/healthz" {
			id, e := security.Authenticate(s.Config, r, r.URL.Path == "/mcp")
			if e != nil {
				if r.URL.Path == "/mcp" {
					w.Header().Set("WWW-Authenticate", `Bearer realm="Worklog MCP"`)
				} else {
					w.Header().Set("WWW-Authenticate", `Basic realm="Worklog", charset="UTF-8"`)
				}
				s.fail(ww, r, e)
				return
			}
			r = r.WithContext(security.WithIdentity(r.Context(), id))
		}
		// 仅已认证的业务请求消耗共用额度，避免静态资源和健康检查
		// 挤占 API/MCP 的调用机会。
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" {
			release, e := s.limiter.Acquire()
			if e != nil {
				s.fail(ww, r, e)
				return
			}
			defer release()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		limit := int64(10 << 20)
		if strings.HasPrefix(r.URL.Path, "/api/worklog/") {
			limit = 1 << 20
		}
		if r.URL.Path == "/mcp" {
			limit = 2 << 20
		}
		if r.ContentLength > limit {
			s.fail(ww, r, &domain.Error{Status: 413, Code: "PAYLOAD_TOO_LARGE", Message: "请求正文过大"})
			return
		}
		r.Body = http.MaxBytesReader(ww, r.Body, limit)
		next.ServeHTTP(ww, r)
	})
}
func respond(w http.ResponseWriter, out any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := domain.Public(err)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		e = &domain.Error{Status: 503, Code: "TIMEOUT", Message: "操作超时，请重新读取确认保存结果"}
	}
	if e.Status >= 500 {
		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}
		fields := append([]any{"requestId", r.Context().Value(requestKey{}), "method", r.Method, "route", route, "code", e.Code}, diagnostics.Fields(err)...)
		s.Logger.Error("operation_failed", fields...)
	}
	if e.Status == 429 || e.Status == 503 {
		w.Header().Set("Retry-After", "2")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": e, "requestId": r.Context().Value(requestKey{})})
}
func body(r *http.Request) (json.RawMessage, error) {
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		return nil, domain.Bad("Content-Type 必须为 application/json")
	}
	b, e := io.ReadAll(r.Body)
	if e != nil {
		var max *http.MaxBytesError
		if errors.As(e, &max) {
			return nil, &domain.Error{Status: 413, Code: "PAYLOAD_TOO_LARGE", Message: "请求正文过大"}
		}
		return nil, domain.Bad("无法读取请求正文")
	}
	if e = domain.UnambiguousJSON(b); e != nil {
		return nil, e
	}
	return b, nil
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		s.fail(w, r, domain.Bad("此接口不接受查询参数"))
		return
	}
	out, e := s.Service.Dashboard(r.Context(), security.Identity(r.Context()))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	respond(w, out)
}
func (s *Server) saveDashboard(w http.ResponseWriter, r *http.Request) {
	b, e := body(r)
	var in domain.Dashboard
	if e == nil {
		e = domain.StrictJSON(b, &in)
	}
	if e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.Service.SaveDashboard(r.Context(), security.Identity(r.Context()), in)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	respond(w, out)
}
func (s *Server) worklog(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	if r.URL.Path == "/api/today" {
		date = s.Service.Today()
	}
	if len(r.URL.Query()) != 0 {
		s.fail(w, r, domain.Bad("此接口不接受查询参数"))
		return
	}
	out, e := s.Service.Worklog(r.Context(), security.Identity(r.Context()), date)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	respond(w, out)
}
func (s *Server) saveWorklog(w http.ResponseWriter, r *http.Request) {
	b, e := body(r)
	var in struct {
		Content json.RawMessage `json:"content"`
		Version int64           `json:"version"`
	}
	if e == nil {
		e = domain.StrictJSON(b, &in, "content", "version")
	}
	if e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.Service.SaveWorklog(r.Context(), security.Identity(r.Context()), chi.URLParam(r, "date"), in.Content, in.Version)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	respond(w, out)
}
func (s *Server) dates(w http.ResponseWriter, r *http.Request) {
	for k, v := range r.URL.Query() {
		if k != "startDate" && k != "endDate" || len(v) != 1 {
			s.fail(w, r, domain.Bad("日期查询参数非法"))
			return
		}
	}
	out, e := s.Service.Dates(r.Context(), security.Identity(r.Context()), r.URL.Query().Get("startDate"), r.URL.Query().Get("endDate"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	respond(w, map[string]any{"dates": out})
}

// operation 将查询参数、路径 ID、版本别名及幂等请求头转换为统一业务参数。
// 这里只适配 HTTP 协议，具体权限和业务约束仍由 Service 校验。
func (s *Server) operation(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fields := map[string]json.RawMessage{}
		if r.Method == http.MethodGet {
			for k, v := range r.URL.Query() {
				if len(v) != 1 {
					s.fail(w, r, domain.Bad("不允许重复查询参数"))
					return
				}
				var b []byte
				if k == "done" {
					if v[0] != "true" && v[0] != "false" {
						s.fail(w, r, domain.Bad("done 必须为 true 或 false"))
						return
					}
					b = []byte(v[0])
				} else {
					b, _ = json.Marshal(v[0])
				}
				fields[k] = b
			}
		} else {
			b, e := body(r)
			if e != nil {
				s.fail(w, r, e)
				return
			}
			if e = json.Unmarshal(b, &fields); e != nil || fields == nil {
				s.fail(w, r, domain.Bad("请求必须为 JSON 对象"))
				return
			}
		}
		if id := chi.URLParam(r, "id"); id != "" {
			// Chi 优先使用 RawPath 匹配路由，此时只解码一次转义字符；
			// Path 已解码时不能再次处理其中的百分号，避免改变真实 ID。
			if r.URL.RawPath != "" {
				var e error
				id, e = url.PathUnescape(id)
				if e != nil {
					s.fail(w, r, domain.Bad("路径 ID 编码非法"))
					return
				}
			}
			if _, ok := fields["id"]; ok {
				s.fail(w, r, domain.Bad("ID 只能通过路径指定"))
				return
			}
			fields["id"], _ = json.Marshal(id)
		}
		for _, alias := range []struct{ from, to string }{{"version", "expectedVersion"}, {"projectRevision", "expectedRevision"}, {"categoryRevision", "expectedRevision"}} {
			if v, ok := fields[alias.from]; ok {
				if _, exists := fields[alias.to]; exists {
					s.fail(w, r, domain.Bad("并发版本参数重复"))
					return
				}
				fields[alias.to] = v
				delete(fields, alias.from)
			}
		}
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			if b, ok := fields["idempotencyKey"]; ok {
				var existing string
				if json.Unmarshal(b, &existing) != nil || existing != key {
					s.fail(w, r, domain.Bad("幂等键不一致"))
					return
				}
			}
			fields["idempotencyKey"], _ = json.Marshal(key)
		}
		b, _ := json.Marshal(fields)
		out, e := s.Service.Execute(r.Context(), security.Identity(r.Context()), op, b)
		if e != nil {
			s.fail(w, r, e)
			return
		}
		respond(w, out)
	}
}

// HTTPServer 在业务超时之外设置连接读写期限，防止慢请求长期占用连接。
// MCP 使用无会话的 JSON 响应，无需为长连接 SSE 放开写入时限。
func HTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
}
