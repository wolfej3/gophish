package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
	"github.com/gophish/gophish/static"
)

func getFirstCampaign(t *testing.T) models.Campaign {
	campaigns, err := models.GetCampaigns(1)
	if err != nil {
		t.Fatalf("error getting first campaign from database: %v", err)
	}
	return campaigns[0]
}

func getFirstEmailRequest(t *testing.T) models.EmailRequest {
	campaign := getFirstCampaign(t)
	req := models.EmailRequest{
		TemplateId:    campaign.TemplateId,
		Template:      campaign.Template,
		PageId:        campaign.PageId,
		Page:          campaign.Page,
		URL:           "http://localhost.localdomain",
		UserId:        1,
		TeamId:        1,
		BaseRecipient: campaign.Results[0].BaseRecipient,
		SMTP:          campaign.SMTP,
		FromAddress:   campaign.SMTP.FromAddress,
	}
	err := models.PostEmailRequest(&req)
	if err != nil {
		t.Fatalf("error creating email request: %v", err)
	}
	return req
}

func openEmail(t *testing.T, ctx *testContext, rid string) {
	resp, err := http.Get(fmt.Sprintf("%s/track?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting /track endpoint: %v", err)
	}
	defer resp.Body.Close()
	got, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading response body from /track endpoint: %v", err)
	}
	expected, err := static.FS.ReadFile("images/pixel.png")
	if err != nil {
		t.Fatalf("error reading local transparent pixel: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("unexpected tracking pixel data received. expected %#v got %#v", expected, got)
	}
}

func openEmail404(t *testing.T, ctx *testContext, rid string) {
	resp, err := http.Get(fmt.Sprintf("%s/track?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting /track endpoint: %v", err)
	}
	defer resp.Body.Close()
	got := resp.StatusCode
	expected := http.StatusNotFound
	if got != expected {
		t.Fatalf("invalid status code received for /track endpoint. expected %d got %d", expected, got)
	}
}

func reportedEmail(t *testing.T, ctx *testContext, rid string) {
	resp, err := http.Get(fmt.Sprintf("%s/report?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting /report endpoint: %v", err)
	}
	got := resp.StatusCode
	expected := http.StatusNoContent
	if got != expected {
		t.Fatalf("invalid status code received for /report endpoint. expected %d got %d", expected, got)
	}
}

func reportEmail404(t *testing.T, ctx *testContext, rid string) {
	resp, err := http.Get(fmt.Sprintf("%s/report?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting /report endpoint: %v", err)
	}
	got := resp.StatusCode
	expected := http.StatusNotFound
	if got != expected {
		t.Fatalf("invalid status code received for /report endpoint. expected %d got %d", expected, got)
	}
}

func clickLink(t *testing.T, ctx *testContext, rid string, expectedHTML string) {
	resp, err := http.Get(fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting / endpoint: %v", err)
	}
	defer resp.Body.Close()
	got, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading payload from / endpoint response: %v", err)
	}
	if !bytes.Equal(got, []byte(expectedHTML)) {
		t.Fatalf("invalid response received from / endpoint. expected %s got %s", got, expectedHTML)
	}
}

func clickLink404(t *testing.T, ctx *testContext, rid string) {
	resp, err := http.Get(fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting / endpoint: %v", err)
	}
	defer resp.Body.Close()
	got := resp.StatusCode
	expected := http.StatusNotFound
	if got != expected {
		t.Fatalf("invalid status code received for / endpoint. expected %d got %d", expected, got)
	}
}

func transparencyRequest(t *testing.T, ctx *testContext, r models.Result, rid, path string) {
	resp, err := http.Get(fmt.Sprintf("%s%s?%s=%s", ctx.phishServer.URL, path, models.RecipientParameter, rid))
	if err != nil {
		t.Fatalf("error requesting %s endpoint: %v", path, err)
	}
	defer resp.Body.Close()
	got := resp.StatusCode
	expected := http.StatusOK
	if got != expected {
		t.Fatalf("invalid status code received for / endpoint. expected %d got %d", expected, got)
	}
	tr := &TransparencyResponse{}
	err = json.NewDecoder(resp.Body).Decode(tr)
	if err != nil {
		t.Fatalf("error unmarshaling transparency request: %v", err)
	}
	expectedResponse := &TransparencyResponse{
		ContactAddress: ctx.config.ContactAddress,
		SendDate:       r.SendDate,
		Server:         config.ServerName,
	}
	if !reflect.DeepEqual(tr, expectedResponse) {
		t.Fatalf("unexpected transparency response received. expected %v got %v", expectedResponse, tr)
	}
}

func TestOpenedPhishingEmail(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	result := campaign.Results[0]
	if result.Status != models.StatusSending {
		t.Fatalf("unexpected result status received. expected %s got %s", models.StatusSending, result.Status)
	}

	openEmail(t, ctx, result.RId)

	campaign = getFirstCampaign(t)
	result = campaign.Results[0]
	lastEvent := campaign.Events[len(campaign.Events)-1]
	if result.Status != models.EventOpened {
		t.Fatalf("unexpected result status received. expected %s got %s", models.EventOpened, result.Status)
	}
	if lastEvent.Message != models.EventOpened {
		t.Fatalf("unexpected event status received. expected %s got %s", lastEvent.Message, models.EventOpened)
	}
	if result.ModifiedDate != lastEvent.Time {
		t.Fatalf("unexpected result modified date received. expected %s got %s", lastEvent.Time, result.ModifiedDate)
	}
}

func TestReportedPhishingEmail(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	result := campaign.Results[0]
	if result.Status != models.StatusSending {
		t.Fatalf("unexpected result status received. expected %s got %s", models.StatusSending, result.Status)
	}

	reportedEmail(t, ctx, result.RId)

	campaign = getFirstCampaign(t)
	result = campaign.Results[0]
	lastEvent := campaign.Events[len(campaign.Events)-1]

	if result.Reported != true {
		t.Fatalf("unexpected result report status received. expected %v got %v", true, result.Reported)
	}
	if lastEvent.Message != models.EventReported {
		t.Fatalf("unexpected event status received. expected %s got %s", lastEvent.Message, models.EventReported)
	}
	if result.ModifiedDate != lastEvent.Time {
		t.Fatalf("unexpected result modified date received. expected %s got %s", lastEvent.Time, result.ModifiedDate)
	}
}

func TestClickedPhishingLinkAfterOpen(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	result := campaign.Results[0]
	if result.Status != models.StatusSending {
		t.Fatalf("unexpected result status received. expected %s got %s", models.StatusSending, result.Status)
	}

	openEmail(t, ctx, result.RId)
	clickLink(t, ctx, result.RId, campaign.Page.HTML)

	campaign = getFirstCampaign(t)
	result = campaign.Results[0]
	lastEvent := campaign.Events[len(campaign.Events)-1]
	if result.Status != models.EventClicked {
		t.Fatalf("unexpected result status received. expected %s got %s", models.EventClicked, result.Status)
	}
	if lastEvent.Message != models.EventClicked {
		t.Fatalf("unexpected event status received. expected %s got %s", lastEvent.Message, models.EventClicked)
	}
	if result.ModifiedDate != lastEvent.Time {
		t.Fatalf("unexpected result modified date received. expected %s got %s", lastEvent.Time, result.ModifiedDate)
	}
}

func TestNoRecipientID(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp, err := http.Get(fmt.Sprintf("%s/track", ctx.phishServer.URL))
	if err != nil {
		t.Fatalf("error requesting /track endpoint: %v", err)
	}
	got := resp.StatusCode
	expected := http.StatusNotFound
	if got != expected {
		t.Fatalf("invalid status code received for /track endpoint. expected %d got %d", expected, got)
	}

	resp, err = http.Get(ctx.phishServer.URL)
	if err != nil {
		t.Fatalf("error requesting /track endpoint: %v", err)
	}
	got = resp.StatusCode
	if got != expected {
		t.Fatalf("invalid status code received for / endpoint. expected %d got %d", expected, got)
	}
}

func TestInvalidRecipientID(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	rid := "XXXXXXXXXX"
	openEmail404(t, ctx, rid)
	clickLink404(t, ctx, rid)
	reportEmail404(t, ctx, rid)
}

func TestCompletedCampaignClick(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	result := campaign.Results[0]
	if result.Status != models.StatusSending {
		t.Fatalf("unexpected result status received. expected %s got %s", models.StatusSending, result.Status)
	}

	openEmail(t, ctx, result.RId)

	campaign = getFirstCampaign(t)
	result = campaign.Results[0]
	if result.Status != models.EventOpened {
		t.Fatalf("unexpected result status received. expected %s got %s", models.EventOpened, result.Status)
	}

	models.CompleteCampaign(campaign.Id, 1)
	openEmail404(t, ctx, result.RId)
	clickLink404(t, ctx, result.RId)

	campaign = getFirstCampaign(t)
	result = campaign.Results[0]
	if result.Status != models.EventOpened {
		t.Fatalf("unexpected result status received. expected %s got %s", models.EventOpened, result.Status)
	}
}

func TestRobotsHandler(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	resp, err := http.Get(fmt.Sprintf("%s/robots.txt", ctx.phishServer.URL))
	if err != nil {
		t.Fatalf("error requesting /robots.txt endpoint: %v", err)
	}
	defer resp.Body.Close()
	got := resp.StatusCode
	expectedStatus := http.StatusOK
	if got != expectedStatus {
		t.Fatalf("invalid status code received for /track endpoint. expected %d got %d", expectedStatus, got)
	}
	expected := []byte("User-agent: *\nDisallow: /\n")
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading response body from /robots.txt endpoint: %v", err)
	}
	if !bytes.Equal(body, expected) {
		t.Fatalf("invalid robots.txt response received. expected %s got %s", expected, body)
	}
}

func TestInvalidPreviewID(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	bogusRId := fmt.Sprintf("%sbogus", models.PreviewPrefix)
	openEmail404(t, ctx, bogusRId)
	clickLink404(t, ctx, bogusRId)
	reportEmail404(t, ctx, bogusRId)
}

func TestPreviewTrack(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	req := getFirstEmailRequest(t)
	openEmail(t, ctx, req.RId)
}

func TestPreviewClick(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	req := getFirstEmailRequest(t)
	clickLink(t, ctx, req.RId, req.Page.HTML)
}

func TestInvalidTransparencyRequest(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	bogusRId := fmt.Sprintf("bogus%s", TransparencySuffix)
	openEmail404(t, ctx, bogusRId)
	clickLink404(t, ctx, bogusRId)
	reportEmail404(t, ctx, bogusRId)
}

func TestTransparencyRequest(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	campaign := getFirstCampaign(t)
	result := campaign.Results[0]
	rid := fmt.Sprintf("%s%s", result.RId, TransparencySuffix)
	transparencyRequest(t, ctx, result, rid, "/")
	transparencyRequest(t, ctx, result, rid, "/track")
	transparencyRequest(t, ctx, result, rid, "/report")

	// And check with the URL encoded version of a +
	rid = fmt.Sprintf("%s%s", result.RId, "%2b")
	transparencyRequest(t, ctx, result, rid, "/")
	transparencyRequest(t, ctx, result, rid, "/track")
	transparencyRequest(t, ctx, result, rid, "/report")
}

func TestRedirectTemplating(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	p := models.Page{
		Name:        "Redirect Page",
		HTML:        "<html>Test</html>",
		UserId:      1,
		TeamId:      1,
		RedirectURL: "http://example.com/{{.RId}}",
	}
	err := models.PostPage(&p)
	if err != nil {
		t.Fatalf("error posting new page: %v", err)
	}
	smtp, _ := models.GetSMTP(1, 1)
	template, _ := models.GetTemplate(1, 1)
	group, _ := models.GetGroup(1, 1)

	campaign := models.Campaign{Name: "Redirect campaign"}
	campaign.UserId = 1
	campaign.TeamId = 1
	campaign.Template = template
	campaign.Page = p
	campaign.SMTP = smtp
	campaign.Groups = []models.Group{group}
	err = models.PostCampaign(&campaign, campaign.UserId, 1)
	if err != nil {
		t.Fatalf("error creating campaign: %v", err)
	}

	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	result := campaign.Results[0]
	resp, err := client.PostForm(fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, result.RId), url.Values{"username": {"test"}, "password": {"test"}})
	if err != nil {
		t.Fatalf("error requesting / endpoint: %v", err)
	}
	defer resp.Body.Close()
	got := resp.StatusCode
	expectedStatus := http.StatusFound
	if got != expectedStatus {
		t.Fatalf("invalid status code received for /track endpoint. expected %d got %d", expectedStatus, got)
	}
	expectedURL := fmt.Sprintf("http://example.com/%s", result.RId)
	gotURL, err := resp.Location()
	if err != nil {
		t.Fatalf("error getting Location header from response: %v", err)
	}
	if gotURL.String() != expectedURL {
		t.Fatalf("invalid redirect received. expected %s got %s", expectedURL, gotURL)
	}
}

// TestMFALureTwoStep verifies the MFA lure's two-step flow: the first
// (credential) POST serves the verification-code prompt rather than
// redirecting, and the second POST (carrying the MFA marker) records a
// "Submitted MFA Code" event and then redirects.
func TestMFALureTwoStep(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	p := models.Page{
		Name:        "MFA Lure Page",
		HTML:        "<html><body><form><input name=\"username\"/><input name=\"password\" type=\"password\"/></form></body></html>",
		UserId:      1,
		TeamId:      1,
		CaptureMFA:  true,
		RedirectURL: "http://example.com/done",
	}
	if err := models.PostPage(&p); err != nil {
		t.Fatalf("error posting new page: %v", err)
	}
	smtp, _ := models.GetSMTP(1, 1)
	template, _ := models.GetTemplate(1, 1)
	group, _ := models.GetGroup(1, 1)

	campaign := models.Campaign{Name: "MFA campaign"}
	campaign.UserId = 1
	campaign.TeamId = 1
	campaign.Template = template
	campaign.Page = p
	campaign.SMTP = smtp
	campaign.Groups = []models.Group{group}
	if err := models.PostCampaign(&campaign, campaign.UserId, 1); err != nil {
		t.Fatalf("error creating campaign: %v", err)
	}
	result := campaign.Results[0]

	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Step 1: credential submit. Expect the verification-code prompt, not a
	// redirect.
	resp, err := client.PostForm(fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, result.RId), url.Values{"username": {"test"}, "password": {"secret"}})
	if err != nil {
		t.Fatalf("error posting credentials: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("step 1: expected 200, got %d", resp.StatusCode)
	}
	body, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(body, []byte(models.MFAMarkerField)) {
		t.Fatalf("step 1: verification-code prompt missing the MFA marker field")
	}

	// Step 2: verification-code submit (carries the marker). Expect a redirect.
	resp, err = client.PostForm(fmt.Sprintf("%s/?%s=%s", ctx.phishServer.URL, models.RecipientParameter, result.RId), url.Values{models.MFAMarkerField: {"1"}, "mfa_code": {"123456"}})
	if err != nil {
		t.Fatalf("error posting mfa code: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step 2: expected redirect (302), got %d", resp.StatusCode)
	}
	gotURL, err := resp.Location()
	if err != nil {
		t.Fatalf("error getting Location header: %v", err)
	}
	if gotURL.String() != "http://example.com/done" {
		t.Fatalf("step 2: invalid redirect, got %s", gotURL)
	}

	// A "Submitted MFA Code" event was recorded for the recipient.
	reloaded, err := models.GetCampaign(campaign.Id, 1)
	if err != nil {
		t.Fatalf("error reloading campaign: %v", err)
	}
	var mfaEvents int
	for _, e := range reloaded.Events {
		if e.Message == models.EventMFASubmit && e.Email == result.Email {
			mfaEvents++
		}
	}
	if mfaEvents != 1 {
		t.Fatalf("expected 1 Submitted MFA Code event, got %d", mfaEvents)
	}
}
