package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"worklog/internal/service"
	"worklog/internal/transport/rest"
)

type hostTransport struct {
	base http.RoundTripper
	host string
}

func (h hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = h.host
	return h.base.RoundTrip(r)
}
func TestRemoteAuthAndReverseProxyMCP(t *testing.T) {
	f := setup(t, "sqlite")
	c := f.cfg
	c.Local = false
	c.BasicUser = "worklog"
	c.BasicPassword = strings.Repeat("private", 4)
	c.AllowedHosts = append(c.AllowedHosts, "worklog.example.com")
	c.AllowedOrigins = append(c.AllowedOrigins, "https://worklog.example.com")
	calendar, e := service.LoadCalendar("")
	must(t, e)
	server := httptest.NewServer(rest.New(f.svc, c, calendar, logger))
	defer server.Close()
	old := f.server
	f.server = server
	defer func() { f.server = old }()
	code, _, h := req(t, f, "GET", "/api/dashboard", nil, nil)
	if code != 401 || h.Get("WWW-Authenticate") == "" {
		t.Fatal(code, h)
	}
	code, _, h = req(t, f, "POST", "/mcp", map[string]any{}, nil)
	if code != 401 || !strings.HasPrefix(h.Get("WWW-Authenticate"), "Bearer") {
		t.Fatal(code, h)
	}
	r, e := http.NewRequest("GET", server.URL+"/api/dashboard", nil)
	must(t, e)
	r.SetBasicAuth(c.BasicUser, c.BasicPassword)
	response, e := http.DefaultClient.Do(r)
	must(t, e)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("browser Basic auth", response.StatusCode)
	}
	// Simulate a loopback HTTPS reverse proxy preserving a public Host.
	// Our exact Host/Origin gate replaces the SDK's loopback hostname heuristic.
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: hostTransport{bearerTransport{http.DefaultTransport, c.WriteToken}, "worklog.example.com"}}}, nil)
	must(t, e)
	defer session.Close()
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	must(t, e)
	if result.IsError {
		t.Fatal(result.Content)
	}
}
