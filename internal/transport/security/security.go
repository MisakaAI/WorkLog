package security

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"worklog/internal/config"
	"worklog/internal/domain"
)

type identityKey struct{}

func WithIdentity(ctx context.Context, id domain.Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
func Identity(ctx context.Context) domain.Identity {
	id, _ := ctx.Value(identityKey{}).(domain.Identity)
	return id
}
func equal(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
// Authenticate 将凭证转换为服务端身份；写入权限与删除类操作权限分别授予。
func Authenticate(c config.Config, r *http.Request, mcp bool) (domain.Identity, error) {
	id := domain.Identity{User: c.User}
	token := ""
	if scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		token = strings.TrimSpace(value)
	}
	if equal(token, c.WriteToken) {
		id.Write = true
		// Bearer 凭证不能通过切换接口获得额外权限。
		// REST 的删除类操作只允许人工使用的 Basic 或本机免密通道。
		id.Destructive = mcp && c.MCPDestructive
		return id, nil
	}
	if equal(token, c.ReadToken) {
		return id, nil
	}
	if !mcp {
		user, pass, ok := r.BasicAuth()
		if ok && equal(user, c.BasicUser) && equal(pass, c.BasicPassword) {
			id.Write = true
			id.Destructive = true
			return id, nil
		}
	}
	// 本机免密访问必须同时满足监听配置、Host 和直连来源均可信；
	// 不使用可伪造的代理转发头判断来源，也不忽略显式提供的错误凭证。
	if !mcp && c.Local && c.BasicPassword == "" && r.Header.Get("Authorization") == "" && loopbackHost(r.Host) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err == nil && ip != nil && ip.IsLoopback() {
			id.Write = true
			id.Destructive = true
			return id, nil
		}
	}
	return domain.Identity{}, &domain.Error{Status: 401, Code: "UNAUTHENTICATED", Message: "需要可信身份认证"}
}
func loopbackHost(host string) bool {
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
}
// Trusted 在身份认证前检查 Host 和 Origin，降低 DNS 重绑定及跨站请求风险。
func Trusted(c config.Config, r *http.Request) error {
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	allowed := false
	for _, v := range c.AllowedHosts {
		if strings.EqualFold(v, host) {
			allowed = true
		}
	}
	if !allowed {
		return domain.Forbidden()
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return domain.Forbidden()
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		allowed = origin == scheme+"://"+r.Host
		for _, o := range c.AllowedOrigins {
			if origin == o {
				allowed = true
			}
		}
		if !allowed {
			return domain.Forbidden()
		}
	}
	// 允许从外站链接打开页面，但拒绝跨站脚本或子资源请求借用本机访问权限
	// 或浏览器自动附带的 Basic 凭证；页面导航也不能绕过 Host/Origin 校验。
	navigation := (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" && origin == "" && !navigation {
		return domain.Forbidden()
	}
	return nil
}

type Limiter struct {
	mu         sync.Mutex
	Start      time.Time
	Count      int
	Concurrent chan struct{}
	Max        int
}

func NewLimiter() *Limiter {
	return &Limiter{Start: time.Now(), Concurrent: make(chan struct{}, 32), Max: 300}
}
// Acquire 同时限制每分钟调用量和并发数；成功后调用方必须执行返回的释放函数。
func (l *Limiter) Acquire() (func(), error) {
	l.mu.Lock()
	if time.Since(l.Start) >= time.Minute {
		l.Start = time.Now()
		l.Count = 0
	}
	if l.Count >= l.Max {
		l.mu.Unlock()
		return nil, &domain.Error{Status: 429, Code: "RATE_LIMITED", Message: "调用过于频繁，请稍后重试"}
	}
	l.Count++
	l.mu.Unlock()
	select {
	case l.Concurrent <- struct{}{}:
		return func() { <-l.Concurrent }, nil
	default:
		return nil, &domain.Error{Status: 429, Code: "RATE_LIMITED", Message: "并发调用过多，请稍后重试"}
	}
}
