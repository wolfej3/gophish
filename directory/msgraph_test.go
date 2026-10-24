package directory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophish/gophish/config"
)

// newGraphServer returns a fake Microsoft Graph that issues a token and
// serves one page of members, with a second page via @odata.nextLink.
func newGraphServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	var baseURL string
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]interface{}{}
		if r.URL.Query().Get("$skiptoken") == "" {
			resp["value"] = []map[string]string{
				{"mail": "alice@example.com", "givenName": "Alice", "surname": "Adams"},
				{"userPrincipalName": "bob@example.com", "givenName": "Bob", "surname": "Baker"},
				{"givenName": "No", "surname": "Email"}, // dropped: no address
			}
			resp["@odata.nextLink"] = baseURL + "/groups/g1/transitiveMembers/microsoft.graph.user?$skiptoken=next"
		} else {
			resp["value"] = []map[string]string{
				{"mail": "carol@example.com", "givenName": "Carol", "surname": "Clark"},
			}
		}
		json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	baseURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func TestMSGraphMembers(t *testing.T) {
	server := newGraphServer(t)
	conn := config.DirectoryConnector{
		Provider: "msgraph", TenantID: "t", ClientID: "c", ClientSecret: "s",
		TokenURL: server.URL + "/token", BaseURL: server.URL,
	}
	members, err := FetchMembers(context.Background(), conn, "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members (paged, no-email dropped), got %d: %+v", len(members), members)
	}
	if members[0].Email != "alice@example.com" || members[0].FirstName != "Alice" {
		t.Fatalf("unexpected first member: %+v", members[0])
	}
	// The user with only a userPrincipalName uses it as the email
	if members[1].Email != "bob@example.com" {
		t.Fatalf("expected UPN fallback, got %q", members[1].Email)
	}
	if members[2].Email != "carol@example.com" {
		t.Fatalf("expected the second page's member, got %q", members[2].Email)
	}
}

func TestMSGraphRequiresConfig(t *testing.T) {
	_, err := newProvider(config.DirectoryConnector{Provider: "msgraph"})
	if err == nil {
		t.Fatal("expected missing config to be rejected")
	}
}

func TestMSGraphReportsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	conn := config.DirectoryConnector{Provider: "msgraph", TenantID: "t", ClientID: "c", ClientSecret: "s", TokenURL: server.URL + "/token", BaseURL: server.URL}
	_, err := FetchMembers(context.Background(), conn, "missing")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a 404 error, got %v", err)
	}
}
