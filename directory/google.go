package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/models"
	"golang.org/x/oauth2/google"
)

// googleDirectoryBaseURL is the Google Admin SDK Directory API base.
const googleDirectoryBaseURL = "https://admin.googleapis.com/admin/directory/v1"

// Google Admin SDK scopes needed to read group members and their names.
var googleScopes = []string{
	"https://www.googleapis.com/auth/admin.directory.group.member.readonly",
	"https://www.googleapis.com/auth/admin.directory.user.readonly",
}

// googleProvider syncs members from a Google Workspace group through the
// Admin SDK Directory API, using a service account with domain-wide
// delegation.
type googleProvider struct {
	client  *http.Client
	baseURL string
}

func newGoogleProvider(conn config.DirectoryConnector) (Provider, error) {
	if conn.CredentialsFile == "" || conn.DelegatedSubject == "" {
		return nil, errors.New("google: credentials_file and delegated_subject are required")
	}
	data, err := os.ReadFile(conn.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("google: reading credentials: %w", err)
	}
	jwtConfig, err := google.JWTConfigFromJSON(data, googleScopes...)
	if err != nil {
		return nil, fmt.Errorf("google: parsing credentials: %w", err)
	}
	// Domain-wide delegation acts as an administrator of the Workspace.
	jwtConfig.Subject = conn.DelegatedSubject
	if conn.TokenURL != "" {
		jwtConfig.TokenURL = conn.TokenURL
	}
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = googleDirectoryBaseURL
	}
	return &googleProvider{
		client:  jwtConfig.Client(context.Background()),
		baseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

type googleMember struct {
	Email string `json:"email"`
	Type  string `json:"type"`
	ID    string `json:"id"`
}

type googleMembersResponse struct {
	Members       []googleMember `json:"members"`
	NextPageToken string         `json:"nextPageToken"`
}

type googleName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type googleUser struct {
	PrimaryEmail string     `json:"primaryEmail"`
	Name         googleName `json:"name"`
}

func (p *googleProvider) Members(ctx context.Context, externalID string) ([]models.BaseRecipient, error) {
	if externalID == "" {
		return nil, errors.New("google: the external_id must be the group's email or ID")
	}
	recipients := []models.BaseRecipient{}
	pageToken := ""
	for {
		link := fmt.Sprintf("%s/groups/%s/members?maxResults=200&includeDerivedMembership=true",
			p.baseURL, url.PathEscape(externalID))
		if pageToken != "" {
			link += "&pageToken=" + url.QueryEscape(pageToken)
		}
		page := googleMembersResponse{}
		if err := p.get(ctx, link, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Members {
			if m.Type != "USER" || m.Email == "" {
				continue
			}
			// The members list has no names, so look up each user.
			first, last := p.lookupName(ctx, m.Email)
			recipients = append(recipients, models.BaseRecipient{
				Email:     m.Email,
				FirstName: first,
				LastName:  last,
			})
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return recipients, nil
}

// lookupName returns the user's first and last name, or empty strings if the
// lookup fails, so a missing name doesn't drop the recipient.
func (p *googleProvider) lookupName(ctx context.Context, email string) (string, string) {
	link := fmt.Sprintf("%s/users/%s?fields=primaryEmail,name", p.baseURL, url.PathEscape(email))
	user := googleUser{}
	if err := p.get(ctx, link, &user); err != nil {
		return "", ""
	}
	return user.Name.GivenName, user.Name.FamilyName
}

func (p *googleProvider) get(ctx context.Context, link string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google: %s returned %s: %s", link, resp.Status, string(body))
	}
	return json.Unmarshal(body, out)
}
