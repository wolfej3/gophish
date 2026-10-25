package controllers

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

func TestIsPreviewBot(t *testing.T) {
	cases := []struct {
		name    string
		ua      string
		headers map[string]string
		want    bool
	}{
		{"facebook preview", "facebookexternalhit/1.1", nil, true},
		{"apple preview", "Mozilla/5.0 (compatible; Applebot/0.1)", nil, true},
		{"whatsapp", "WhatsApp/2.0", nil, true},
		{"proofpoint scanner", "Mozilla/5.0 proofpoint", nil, true},
		{"real safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", nil, false},
		{"real chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", nil, false},
		{"prefetch header", "Mozilla/5.0", map[string]string{"Purpose": "prefetch"}, true},
		{"x-moz prefetch", "Mozilla/5.0", map[string]string{"X-Moz": "prefetch"}, true},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("User-Agent", c.ua)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		if got := isPreviewBot(req); got != c.want {
			t.Errorf("%s: isPreviewBot = %v, want %v", c.name, got, c.want)
		}
	}
}

// getWithUA issues a GET with a specific User-Agent.
func getWithUA(t *testing.T, url, ua string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", ua)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestShortLinkClickServesPageAndRecordsClick(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	rid := campaign.Results[0].RId

	// The masked short link /l/<rid> serves the landing page directly.
	resp := getWithUA(t, fmt.Sprintf("%s/l/%s", ctx.phishServer.URL, rid), "Mozilla/5.0 (iPhone) Safari/604.1")
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	if !bytes.Equal(body, []byte(campaign.Page.HTML)) {
		t.Fatalf("short link did not serve the landing page; got %q", string(body))
	}

	campaign = getFirstCampaign(t)
	if campaign.Results[0].Status != models.EventClicked {
		t.Fatalf("expected status %s after short-link click, got %s", models.EventClicked, campaign.Results[0].Status)
	}
}

func TestPreviewBotDoesNotRecordClick(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	rid := campaign.Results[0].RId

	// A link-preview bot fetch must not record a click and must not serve the
	// real landing page.
	resp := getWithUA(t, fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid), "facebookexternalhit/1.1")
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	if bytes.Contains(body, []byte(campaign.Page.HTML)) && len(campaign.Page.HTML) > 0 {
		t.Fatalf("preview bot was served the real landing page")
	}

	campaign = getFirstCampaign(t)
	if campaign.Results[0].Status != models.StatusSending {
		t.Fatalf("preview bot changed result status to %s (expected it to stay %s)", campaign.Results[0].Status, models.StatusSending)
	}
}

func TestPreviewBotDoesNotRecordOpen(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	rid := campaign.Results[0].RId

	resp := getWithUA(t, fmt.Sprintf("%s/track?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid), "Applebot/0.1")
	resp.Body.Close()

	campaign = getFirstCampaign(t)
	if campaign.Results[0].Status != models.StatusSending {
		t.Fatalf("preview bot changed result status to %s via /track (expected %s)", campaign.Results[0].Status, models.StatusSending)
	}
}
