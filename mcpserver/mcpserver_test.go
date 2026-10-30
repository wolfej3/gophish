package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubRoundTripper records the last request and returns a canned response, so
// the client and tools can be tested without a running server or open port.
type stubRoundTripper struct {
	lastReq  *http.Request
	lastBody []byte
	status   int
	respBody string
}

func (s *stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	s.lastReq = r
	if r.Body != nil {
		s.lastBody, _ = io.ReadAll(r.Body)
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(s.respBody)),
		Header:     make(http.Header),
	}, nil
}

func newStubClient(stub *stubRoundTripper, defaultTeam string) *Client {
	c := newClient("https://gophish.test/", "gpk_secret", defaultTeam, true)
	c.HTTPClient.Transport = stub
	return c
}

func TestClientDoHeadersAndPath(t *testing.T) {
	stub := &stubRoundTripper{respBody: `[{"id":1}]`}
	c := newStubClient(stub, "7")

	if _, err := c.do(context.Background(), "GET", "/campaigns/", "", nil); err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if stub.lastReq.Method != "GET" {
		t.Errorf("expected GET, got %s", stub.lastReq.Method)
	}
	if got := stub.lastReq.URL.Path; got != "/api/campaigns/" {
		t.Errorf("expected path /api/campaigns/, got %s", got)
	}
	if got := stub.lastReq.Header.Get("Authorization"); got != "Bearer gpk_secret" {
		t.Errorf("expected bearer token, got %q", got)
	}
	if got := stub.lastReq.Header.Get(teamIDHeader); got != "7" {
		t.Errorf("expected default team header 7, got %q", got)
	}

	// A per-call team overrides the default.
	if _, err := c.do(context.Background(), "GET", "/campaigns/", "9", nil); err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if got := stub.lastReq.Header.Get(teamIDHeader); got != "9" {
		t.Errorf("expected overridden team header 9, got %q", got)
	}
}

func TestClientDoBody(t *testing.T) {
	stub := &stubRoundTripper{respBody: `{"id":1}`}
	c := newStubClient(stub, "")

	body := map[string]string{"name": "Example"}
	if _, err := c.do(context.Background(), "POST", "/groups/", "", body); err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if got := stub.lastReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected JSON content type, got %q", got)
	}
	var sent map[string]string
	if err := json.Unmarshal(stub.lastBody, &sent); err != nil {
		t.Fatalf("body was not JSON: %v", err)
	}
	if sent["name"] != "Example" {
		t.Errorf("expected body name Example, got %q", sent["name"])
	}
	// No team configured and none supplied: the header must be absent.
	if _, ok := stub.lastReq.Header[teamIDHeader]; ok {
		t.Errorf("expected no team header when none configured")
	}
}

func TestClientDoErrorMapping(t *testing.T) {
	stub := &stubRoundTripper{status: http.StatusForbidden, respBody: `{"message":"Permission denied"}`}
	c := newStubClient(stub, "")

	_, err := c.do(context.Background(), "POST", "/campaigns/", "", map[string]string{})
	if err == nil {
		t.Fatal("expected an error for a 403 response")
	}
	ae, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if ae.status != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", ae.status)
	}
	if !strings.Contains(ae.Error(), "Permission denied") || !strings.Contains(ae.Error(), "403") {
		t.Errorf("error message missing status or API message: %q", ae.Error())
	}
}

// connectTools wires an in-memory client session to a server with the tools
// registered against the given stub-backed REST client.
func connectTools(t *testing.T, c *Client) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "gophish", Version: "test"}, nil)
	registerTools(server, c)

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result had no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	return tc.Text
}

func TestToolListCampaignsPassthrough(t *testing.T) {
	stub := &stubRoundTripper{respBody: `[{"id":1,"name":"Camp"}]`}
	cs := connectTools(t, newStubClient(stub, ""))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_campaigns"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != `[{"id":1,"name":"Camp"}]` {
		t.Errorf("expected REST JSON passed through, got %q", got)
	}
	if got := stub.lastReq.URL.Path; got != "/api/campaigns/" {
		t.Errorf("expected GET /api/campaigns/, got %s", got)
	}
}

func TestToolCreateCampaignBody(t *testing.T) {
	stub := &stubRoundTripper{respBody: `{"id":5}`}
	cs := connectTools(t, newStubClient(stub, ""))

	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_campaign",
		Arguments: map[string]any{
			"name":     "Q1 Test",
			"template": "Welcome",
			"page":     "Login",
			"smtp":     "Primary",
			"url":      "https://phish.example.com",
			"groups":   []string{"Engineering", "Sales"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if stub.lastReq.Method != "POST" || stub.lastReq.URL.Path != "/api/campaigns/" {
		t.Errorf("expected POST /api/campaigns/, got %s %s", stub.lastReq.Method, stub.lastReq.URL.Path)
	}
	var body struct {
		Name     string              `json:"name"`
		URL      string              `json:"url"`
		Template map[string]string   `json:"template"`
		Page     map[string]string   `json:"page"`
		SMTP     map[string]string   `json:"smtp"`
		Groups   []map[string]string `json:"groups"`
	}
	if err := json.Unmarshal(stub.lastBody, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body.Name != "Q1 Test" || body.URL != "https://phish.example.com" {
		t.Errorf("unexpected scalar fields: %+v", body)
	}
	if body.Template["name"] != "Welcome" || body.Page["name"] != "Login" || body.SMTP["name"] != "Primary" {
		t.Errorf("building blocks not referenced by name: %+v", body)
	}
	if len(body.Groups) != 2 || body.Groups[0]["name"] != "Engineering" || body.Groups[1]["name"] != "Sales" {
		t.Errorf("groups not mapped to {name}: %+v", body.Groups)
	}
}

// TestToolRespectsKeyBoundary proves the API key is the permission boundary:
// when the REST API rejects a write (as it would for a read-only scoped key),
// the tool surfaces that as a tool error carrying the API's message rather
// than enforcing anything itself.
func TestToolRespectsKeyBoundary(t *testing.T) {
	stub := &stubRoundTripper{status: http.StatusForbidden, respBody: `{"message":"Permission denied"}`}
	cs := connectTools(t, newStubClient(stub, ""))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_campaign",
		Arguments: map[string]any{
			"name": "x", "template": "t", "page": "p", "smtp": "s",
			"url": "https://e.test", "groups": []string{"g"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool returned protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected the tool call to report an error")
	}
	if got := resultText(t, res); !strings.Contains(got, "Permission denied") {
		t.Errorf("expected API message surfaced, got %q", got)
	}
}
