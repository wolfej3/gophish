package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
	"golang.org/x/oauth2/clientcredentials"
)

// graphBaseURL is Microsoft Graph's default base address.
const graphBaseURL = "https://graph.microsoft.com/v1.0"

// msgraphProvider syncs members from a Microsoft Entra ID (Azure AD) group
// through Microsoft Graph, using an app registration's client credentials.
type msgraphProvider struct {
	client  *http.Client
	baseURL string
}

func newMSGraphProvider(conn config.DirectoryConnector) (Provider, error) {
	if conn.TenantID == "" || conn.ClientID == "" || conn.ClientSecret == "" {
		return nil, errors.New("msgraph: tenant_id, client_id, and client_secret are required")
	}
	tokenURL := conn.TokenURL
	if tokenURL == "" {
		tokenURL = fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", conn.TenantID)
	}
	cc := &clientcredentials.Config{
		ClientID:     conn.ClientID,
		ClientSecret: conn.ClientSecret,
		TokenURL:     tokenURL,
		Scopes:       []string{"https://graph.microsoft.com/.default"},
	}
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = graphBaseURL
	}
	return &msgraphProvider{
		client:  cc.Client(context.Background()),
		baseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

// graphUser is a user as Microsoft Graph returns it.
type graphUser struct {
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	GivenName         string `json:"givenName"`
	Surname           string `json:"surname"`
}

// graphMembersResponse is a page of group members.
type graphMembersResponse struct {
	Value    []graphUser `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

func (p *msgraphProvider) Members(ctx context.Context, externalID string) ([]models.BaseRecipient, error) {
	if externalID == "" {
		return nil, errors.New("msgraph: the external_id must be the directory group's object ID")
	}
	// Only user members are relevant as recipients; transitiveMembers also
	// picks up users in nested groups.
	next := fmt.Sprintf("%s/groups/%s/transitiveMembers/microsoft.graph.user?$select=mail,userPrincipalName,givenName,surname&$top=999",
		p.baseURL, url.PathEscape(externalID))
	recipients := []models.BaseRecipient{}
	for next != "" {
		page, err := p.fetchPage(ctx, next)
		if err != nil {
			return nil, err
		}
		for _, u := range page.Value {
			email := u.Mail
			if email == "" {
				email = u.UserPrincipalName
			}
			if email == "" {
				continue
			}
			recipients = append(recipients, models.BaseRecipient{
				Email:     email,
				FirstName: u.GivenName,
				LastName:  u.Surname,
			})
		}
		next = page.NextLink
	}
	return recipients, nil
}

func (p *msgraphProvider) fetchPage(ctx context.Context, link string) (*graphMembersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("msgraph: %s returned %s: %s", link, resp.Status, string(body))
	}
	page := &graphMembersResponse{}
	if err := json.Unmarshal(body, page); err != nil {
		return nil, fmt.Errorf("msgraph: parsing response: %w", err)
	}
	return page, nil
}
