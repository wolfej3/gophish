package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// jsonResult wraps a raw JSON body from the REST API as an MCP tool result.
// The API's JSON is passed through verbatim as text content so the model
// sees exactly what the HTTP API returns.
func jsonResult(data json.RawMessage) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

// teamField is embedded in every tool's input so a call may override the
// client's default team for that one request.
type teamField struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"optional team id to act within; overrides the server's default team for this call"`
}

type emptyInput struct {
	teamField
}

type idInput struct {
	teamField
	ID int64 `json:"id" jsonschema:"the numeric id of the object"`
}

type emailInput struct {
	teamField
	Email string `json:"email" jsonschema:"the target's email address"`
}

type createCampaignInput struct {
	teamField
	Name       string   `json:"name" jsonschema:"a name for the campaign"`
	Template   string   `json:"template" jsonschema:"the name of an existing email template"`
	Page       string   `json:"page" jsonschema:"the name of an existing landing page"`
	SMTP       string   `json:"smtp" jsonschema:"the name of an existing sending profile"`
	URL        string   `json:"url" jsonschema:"the phishing server URL targets will reach, e.g. https://phish.example.com"`
	Groups     []string `json:"groups" jsonschema:"names of existing target groups to send to"`
	LaunchDate string   `json:"launch_date,omitempty" jsonschema:"RFC3339 time to launch; omit to launch immediately"`
	SendByDate string   `json:"send_by_date,omitempty" jsonschema:"RFC3339 time by which all emails should be sent; optional"`
}

type createTemplateInput struct {
	teamField
	Name           string `json:"name" jsonschema:"a name for the template"`
	Subject        string `json:"subject" jsonschema:"the email subject line"`
	Text           string `json:"text,omitempty" jsonschema:"the plaintext body; optional"`
	HTML           string `json:"html,omitempty" jsonschema:"the HTML body; optional"`
	EnvelopeSender string `json:"envelope_sender,omitempty" jsonschema:"optional envelope-from override"`
}

type createPageInput struct {
	teamField
	Name               string `json:"name" jsonschema:"a name for the landing page"`
	HTML               string `json:"html" jsonschema:"the landing page HTML"`
	CaptureCredentials bool   `json:"capture_credentials,omitempty" jsonschema:"capture submitted form data"`
	CapturePasswords   bool   `json:"capture_passwords,omitempty" jsonschema:"also capture submitted passwords (requires capture_credentials)"`
	RedirectURL        string `json:"redirect_url,omitempty" jsonschema:"URL to redirect to after submission; optional"`
}

type targetInput struct {
	Email     string `json:"email" jsonschema:"the target's email address"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Position  string `json:"position,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

type createGroupInput struct {
	teamField
	Name    string        `json:"name" jsonschema:"a name for the target group"`
	Targets []targetInput `json:"targets" jsonschema:"the people in the group"`
}

// registerTools registers the curated toolset on the MCP server. Every tool
// is a REST call carrying the configured API key, so the key's scope and the
// view-only rule decide what actually succeeds — a read-only or scoped key
// makes the write tools fail server-side with a permission error.
func registerTools(server *mcp.Server, c *Client) {
	// --- Campaigns ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_campaigns",
		Description: "List all phishing campaigns visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/campaigns/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_campaigns_summary",
		Description: "Get a summary of all campaigns (counts and high-level stats).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/campaigns/summary", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_campaign",
		Description: "Get a single campaign by id, including its template, page, and sending profile.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/campaigns/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_campaign_results",
		Description: "Get the per-recipient results and timeline for a campaign.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/campaigns/%d/results", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_campaign_summary",
		Description: "Get aggregate statistics for a single campaign.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/campaigns/%d/summary", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_campaign",
		Description: "Create and launch a campaign. Template, page, sending profile, and groups must already exist and are referenced by name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCampaignInput) (*mcp.CallToolResult, any, error) {
		groups := make([]map[string]string, 0, len(in.Groups))
		for _, g := range in.Groups {
			groups = append(groups, map[string]string{"name": g})
		}
		body := map[string]interface{}{
			"name":     in.Name,
			"url":      in.URL,
			"template": map[string]string{"name": in.Template},
			"page":     map[string]string{"name": in.Page},
			"smtp":     map[string]string{"name": in.SMTP},
			"groups":   groups,
		}
		if in.LaunchDate != "" {
			body["launch_date"] = in.LaunchDate
		}
		if in.SendByDate != "" {
			body["send_by_date"] = in.SendByDate
		}
		data, err := c.do(ctx, "POST", "/campaigns/", in.TeamID, body)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_campaign",
		Description: "Mark a campaign as completed, stopping further activity.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/campaigns/%d/complete", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_campaign",
		Description: "Permanently delete a campaign and its results.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "DELETE", fmt.Sprintf("/campaigns/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- Templates ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_templates",
		Description: "List all email templates visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/templates/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_template",
		Description: "Get a single email template by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/templates/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_template",
		Description: "Create an email template.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTemplateInput) (*mcp.CallToolResult, any, error) {
		body := map[string]interface{}{
			"name":            in.Name,
			"subject":         in.Subject,
			"text":            in.Text,
			"html":            in.HTML,
			"envelope_sender": in.EnvelopeSender,
		}
		data, err := c.do(ctx, "POST", "/templates/", in.TeamID, body)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- Landing pages ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_pages",
		Description: "List all landing pages visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/pages/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_page",
		Description: "Get a single landing page by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/pages/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_page",
		Description: "Create a landing page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createPageInput) (*mcp.CallToolResult, any, error) {
		body := map[string]interface{}{
			"name":                in.Name,
			"html":                in.HTML,
			"capture_credentials": in.CaptureCredentials,
			"capture_passwords":   in.CapturePasswords,
			"redirect_url":        in.RedirectURL,
		}
		data, err := c.do(ctx, "POST", "/pages/", in.TeamID, body)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- Sending profiles (read only; profiles carry secrets and transport
	// config and are set up in the UI) ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sending_profiles",
		Description: "List all sending profiles (SMTP/Graph/Gmail/SMS) visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/smtp/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sending_profile",
		Description: "Get a single sending profile by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/smtp/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- Groups ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_groups",
		Description: "List all target groups visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/groups/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_group",
		Description: "Get a single target group by id, including its members.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", fmt.Sprintf("/groups/%d", in.ID), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_group",
		Description: "Create a target group with a list of people.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createGroupInput) (*mcp.CallToolResult, any, error) {
		targets := make([]map[string]string, 0, len(in.Targets))
		for _, t := range in.Targets {
			targets = append(targets, map[string]string{
				"email":      t.Email,
				"first_name": t.FirstName,
				"last_name":  t.LastName,
				"position":   t.Position,
				"phone":      t.Phone,
			})
		}
		body := map[string]interface{}{"name": in.Name, "targets": targets}
		data, err := c.do(ctx, "POST", "/groups/", in.TeamID, body)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- People / recon ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_people",
		Description: "List all people across target groups visible to the API key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/people/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_person",
		Description: "Get a single person by email address.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emailInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/people/"+url.PathEscape(in.Email), in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	// --- Teams / tenants ---
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_teams",
		Description: "List the teams the API key can see.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/teams/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tenants",
		Description: "List all tenants. Succeeds only for a provider-admin key; otherwise the API returns a permission error.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, any, error) {
		data, err := c.do(ctx, "GET", "/tenants/", in.TeamID, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(data), nil, nil
	})
}
