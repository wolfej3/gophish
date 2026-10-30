package mcpserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// These mirror middleware.TeamIDHeader / middleware.TenantIDHeader. They are
// redefined here (rather than imported) so the MCP client stays a thin,
// standalone HTTP client with no dependency on the server packages, the
// database, or config — it talks to a running Gophish purely over the REST
// API. Keep them in sync with middleware/team.go.
const (
	teamIDHeader   = "X-Gophish-Team-Id"
	tenantIDHeader = "X-Gophish-Tenant-Id"
)

// Client is a thin REST client for the Gophish admin API. Every request
// carries the configured API key as a bearer token, so the key's
// permissions, scope, team, and tenant — and the view-only rule — are
// enforced server-side exactly as they are for any other API caller. The API
// key is the permission boundary; this client adds no enforcement of its own.
type Client struct {
	BaseURL     string
	APIKey      string
	DefaultTeam string
	HTTPClient  *http.Client
}

// apiError carries a non-2xx response's status and the API's own message so
// the MCP layer can surface it to the caller.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("gophish API %d: %s", e.status, e.message)
	}
	return fmt.Sprintf("gophish API returned status %d", e.status)
}

// newClient builds a Client. When insecure is true, TLS certificate
// verification is skipped — this is for the self-signed certificate the admin
// server uses by default, not for talking to an untrusted host.
func newClient(baseURL, apiKey, defaultTeam string, insecure bool) *Client {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		DefaultTeam: defaultTeam,
		HTTPClient:  &http.Client{Transport: transport},
	}
}

// do performs a request against the admin API under /api and returns the raw
// JSON body. When team is non-empty it overrides the client's default team
// for this call. A non-2xx response is returned as an *apiError carrying the
// API's message.
func (c *Client) do(ctx context.Context, method, path, team string, body interface{}) (json.RawMessage, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api"+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if team == "" {
		team = c.DefaultTeam
	}
	if team != "" {
		req.Header.Set(teamIDHeader, team)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{status: resp.StatusCode, message: extractMessage(data)}
	}
	return json.RawMessage(data), nil
}

// extractMessage pulls the "message" field out of a Gophish JSON error body,
// falling back to the trimmed raw body when there is no such field.
func extractMessage(data []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return e.Message
	}
	return strings.TrimSpace(string(data))
}
