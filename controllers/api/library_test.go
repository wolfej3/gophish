package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gophish/gophish/config"
	ctx "github.com/gophish/gophish/context"
	"github.com/gophish/gophish/library"
	"github.com/gophish/gophish/models"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newLibraryRepo builds a one-template git repo and returns a synced Library.
func newLibraryRepo(t *testing.T) *library.Library {
	t.Helper()
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib-template/template.json", `{"name":"Lib Template","subject":"Hello {{.FirstName}}","envelope_sender":"it@corp.test"}`)
	write("lib-template/html.html", `<html><body>Hi {{.URL}}</body></html>`)
	write("lib-template/attachments/notice.txt", `attached`)
	wt, _ := repo.Worktree()
	wt.AddGlob(".")
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@e.test", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	head, _ := repo.Head()
	lib := library.New(&config.TemplateLibrary{
		URL:       src,
		Branch:    head.Name().Short(),
		CachePath: filepath.Join(t.TempDir(), "clone"),
	})
	if err := lib.Sync(); err != nil {
		t.Fatal(err)
	}
	return lib
}

// importRequest issues a LibraryImport call as team 1 / user 1.
func importRequest(t *testing.T, srv *Server, name string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/library/import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = ctx.Set(req, "team_id", int64(1))
	req = ctx.Set(req, "user_id", int64(1))
	resp := httptest.NewRecorder()
	srv.LibraryImport(resp, req)
	return resp
}

func TestLibraryImportCopiesTemplate(t *testing.T) {
	setupTest(t)
	srv := NewServer(WithTemplateLibrary(newLibraryRepo(t)))

	resp := importRequest(t, srv, "Lib Template")
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	var created models.Template
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Lib Template" {
		t.Errorf("name = %q", created.Name)
	}
	if created.TeamId != 1 {
		t.Errorf("team = %d, want 1", created.TeamId)
	}
	if created.Subject != "Hello {{.FirstName}}" || created.HTML == "" {
		t.Errorf("bodies not copied: %+v", created)
	}
	if len(created.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(created.Attachments))
	}
	// Attachment content is stored base64-encoded.
	if dec, err := base64.StdEncoding.DecodeString(created.Attachments[0].Content); err != nil || string(dec) != "attached" {
		t.Errorf("attachment content wrong: %q (err %v)", created.Attachments[0].Content, err)
	}

	// The imported template is a real, independent row in the team.
	stored, err := models.GetTemplateByName("Lib Template", 1)
	if err != nil {
		t.Fatalf("imported template not found: %v", err)
	}
	if stored.Id != created.Id {
		t.Errorf("stored id %d != created id %d", stored.Id, created.Id)
	}
}

func TestLibraryImportSuffixesOnCollision(t *testing.T) {
	setupTest(t)
	srv := NewServer(WithTemplateLibrary(newLibraryRepo(t)))

	if importRequest(t, srv, "Lib Template").Code != http.StatusCreated {
		t.Fatal("first import failed")
	}
	resp := importRequest(t, srv, "Lib Template")
	if resp.Code != http.StatusCreated {
		t.Fatalf("second import failed: %s", resp.Body.String())
	}
	var second models.Template
	json.NewDecoder(resp.Body).Decode(&second)
	if second.Name != "Lib Template (2)" {
		t.Errorf("expected suffixed name, got %q", second.Name)
	}
}

func TestLibraryEndpointsUnconfigured(t *testing.T) {
	setupTest(t)
	srv := NewServer() // no library

	req := httptest.NewRequest(http.MethodGet, "/api/library/templates", nil)
	resp := httptest.NewRecorder()
	srv.LibraryTemplates(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when unconfigured, got %d", resp.Code)
	}
}
