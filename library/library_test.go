package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gophish/gophish/config"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// writeFile writes content to path, creating parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll stages and commits the whole worktree, returning the current branch.
func commitAll(t *testing.T, repo *git.Repository, msg string) string {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Name().Short()
}

// newSourceRepo creates a git repo with one template and returns its path and
// current branch.
func newSourceRepo(t *testing.T) (string, string) {
	t.Helper()
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "phishing", "password-reset", "template.json"),
		`{"name":"Password Reset","subject":"Reset your password","envelope_sender":"it@corp.test","description":"Classic reset lure"}`)
	writeFile(t, filepath.Join(src, "phishing", "password-reset", "html.html"),
		`<html><body>Reset: {{.URL}}</body></html>`)
	writeFile(t, filepath.Join(src, "phishing", "password-reset", "text.txt"),
		`Reset: {{.URL}}`)
	writeFile(t, filepath.Join(src, "phishing", "password-reset", "attachments", "notice.txt"),
		`see attached`)
	branch := commitAll(t, repo, "add password reset")
	return src, branch
}

func TestNewReturnsNilWhenUnconfigured(t *testing.T) {
	if New(nil) != nil {
		t.Fatal("expected nil Library for nil config")
	}
	if New(&config.TemplateLibrary{URL: ""}) != nil {
		t.Fatal("expected nil Library for empty URL")
	}
}

func TestSyncCloneListGet(t *testing.T) {
	src, branch := newSourceRepo(t)
	cache := filepath.Join(t.TempDir(), "clone")
	lib := New(&config.TemplateLibrary{URL: src, Branch: branch, CachePath: cache})
	if lib == nil {
		t.Fatal("expected a Library")
	}
	if err := lib.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	list, err := lib.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 template, got %d", len(list))
	}
	got := list[0]
	if got.Name != "Password Reset" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Category != "phishing/password-reset" {
		t.Errorf("category = %q", got.Category)
	}
	if !got.HasHTML || !got.HasText || got.AttachmentCount != 1 {
		t.Errorf("content flags wrong: html=%v text=%v attach=%d", got.HasHTML, got.HasText, got.AttachmentCount)
	}
	// Listing must not leak bodies.
	if got.HTML != "" || got.Text != "" {
		t.Error("List should not populate bodies")
	}

	full, err := lib.Get("Password Reset")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if full.HTML == "" || full.Text == "" {
		t.Error("Get should populate bodies")
	}
	if full.Subject != "Reset your password" || full.EnvelopeSender != "it@corp.test" {
		t.Errorf("metadata wrong: %+v", full)
	}
	if len(full.Attachments) != 1 || full.Attachments[0].Name != "notice.txt" {
		t.Fatalf("attachments wrong: %+v", full.Attachments)
	}
	if string(full.Attachments[0].Content) != "see attached" {
		t.Errorf("attachment content = %q", full.Attachments[0].Content)
	}

	if _, err := lib.Get("Nonexistent"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSyncPullsUpdates(t *testing.T) {
	src, branch := newSourceRepo(t)
	cache := filepath.Join(t.TempDir(), "clone")
	lib := New(&config.TemplateLibrary{URL: src, Branch: branch, CachePath: cache})
	if err := lib.Sync(); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// Add a second template to the source and commit.
	repo, err := git.PlainOpen(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "invoice", "template.json"),
		`{"name":"Invoice","subject":"Invoice attached"}`)
	writeFile(t, filepath.Join(src, "invoice", "text.txt"), `Please pay`)
	commitAll(t, repo, "add invoice")

	if err := lib.Sync(); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	list, err := lib.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 templates after pull, got %d", len(list))
	}
}

func TestListUsesSubPath(t *testing.T) {
	src, branch := newSourceRepo(t)
	// Add a template outside the "phishing" subtree that should be excluded.
	repo, _ := git.PlainOpen(src)
	writeFile(t, filepath.Join(src, "other", "template.json"), `{"name":"Other"}`)
	writeFile(t, filepath.Join(src, "other", "text.txt"), `x`)
	commitAll(t, repo, "add other")

	cache := filepath.Join(t.TempDir(), "clone")
	lib := New(&config.TemplateLibrary{URL: src, Branch: branch, CachePath: cache, Path: "phishing"})
	if err := lib.Sync(); err != nil {
		t.Fatal(err)
	}
	list, err := lib.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Password Reset" {
		t.Fatalf("subpath scan wrong: %+v", list)
	}
}

func TestRootStaysInsideClone(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "clone")
	lib := New(&config.TemplateLibrary{URL: "http://example.test/x.git", CachePath: cache, Path: "../../etc"})
	root, err := lib.root()
	if err != nil {
		t.Fatal(err)
	}
	base, _ := filepath.Abs(cache)
	rel, err := filepath.Rel(base, root)
	if err != nil {
		t.Fatal(err)
	}
	if rel == ".." || filepath.IsAbs(rel) || hasDotDot(rel) {
		t.Fatalf("root escaped the clone: base=%s root=%s rel=%s", base, root, rel)
	}
}

func hasDotDot(p string) bool {
	for _, part := range filepath.SplitList(p) {
		_ = part
	}
	// Simple check: a path that climbs out starts with ".."
	return len(p) >= 2 && p[0] == '.' && p[1] == '.'
}
