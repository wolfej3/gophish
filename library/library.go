// Package library provides a read-only, git-backed catalog of email templates
// that operators can import into their own team as independent copies.
//
// Catalog layout: anywhere under the configured path, a directory that contains
// a "template.json" manifest is one template. Alongside the manifest, an
// optional "html.html" and "text.txt" hold the bodies, and an optional
// "attachments/" directory holds files attached to the email. Directories let
// the repo group templates into categories; template names must be unique
// across the whole catalog.
package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gophish/gophish/config"
	log "github.com/gophish/gophish/logger"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// maxFileSize caps how much of any single catalog file is read into memory,
// guarding against a hostile or corrupt repo exhausting memory. Attachment
// content is further validated against the model's own size cap on import.
const maxFileSize = 25 << 20 // 25 MiB

// manifest is the on-disk template.json.
type manifest struct {
	Name           string `json:"name"`
	Subject        string `json:"subject"`
	EnvelopeSender string `json:"envelope_sender"`
	Description    string `json:"description"`
}

// Attachment is a file attached to a library template.
type Attachment struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content []byte `json:"-"`
}

// Template is one entry in the catalog. Listing returns entries with the body
// fields empty; Get fills them in.
type Template struct {
	Name            string       `json:"name"`
	Subject         string       `json:"subject"`
	EnvelopeSender  string       `json:"envelope_sender"`
	Description     string       `json:"description"`
	Category        string       `json:"category"`
	HasHTML         bool         `json:"has_html"`
	HasText         bool         `json:"has_text"`
	AttachmentCount int          `json:"attachment_count"`
	HTML            string       `json:"html,omitempty"`
	Text            string       `json:"text,omitempty"`
	Attachments     []Attachment `json:"attachments,omitempty"`

	// dir is the absolute directory holding this template's files. It is set
	// internally and never derived from caller input, so Get can't be used to
	// read outside the clone.
	dir string
}

// ErrNotConfigured is returned when no template library is configured.
var ErrNotConfigured = errors.New("template library is not configured")

// ErrNotFound is returned when a named template isn't in the catalog.
var ErrNotFound = errors.New("template not found in library")

// Library is a handle to the configured catalog. It's safe for concurrent use.
type Library struct {
	cfg config.TemplateLibrary
	mu  sync.Mutex
}

// New returns a Library for the given config, or nil if none is configured.
func New(cfg *config.TemplateLibrary) *Library {
	if cfg == nil || strings.TrimSpace(cfg.URL) == "" {
		return nil
	}
	return &Library{cfg: *cfg}
}

func (l *Library) branch() string {
	if l.cfg.Branch != "" {
		return l.cfg.Branch
	}
	return "main"
}

func (l *Library) cachePath() string {
	if l.cfg.CachePath != "" {
		return l.cfg.CachePath
	}
	return "./template_library"
}

// root is the directory scanned for templates: the clone, plus any configured
// subpath. It stays within the clone even if Path contains "..".
func (l *Library) root() (string, error) {
	base, err := filepath.Abs(l.cachePath())
	if err != nil {
		return "", err
	}
	if l.cfg.Path == "" {
		return base, nil
	}
	joined := filepath.Join(base, filepath.Clean("/"+l.cfg.Path))
	return joined, nil
}

func (l *Library) auth() *http.BasicAuth {
	if l.cfg.AuthToken == "" {
		return nil
	}
	user := l.cfg.Username
	if user == "" {
		user = "git"
	}
	return &http.BasicAuth{Username: user, Password: l.cfg.AuthToken}
}

// Sync clones the repository if it isn't present yet, otherwise fetches and
// resets the tracked branch to the remote. It is safe to call repeatedly.
func (l *Library) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sync()
}

func (l *Library) sync() error {
	dir := l.cachePath()
	ref := plumbing.NewBranchReferenceName(l.branch())

	repo, err := git.PlainOpen(dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		log.Infof("Cloning template library from %s", l.cfg.URL)
		_, cloneErr := git.PlainClone(dir, false, &git.CloneOptions{
			URL:           l.cfg.URL,
			ReferenceName: ref,
			SingleBranch:  true,
			Auth:          l.auth(),
		})
		return cloneErr
	}
	if err != nil {
		// The cache exists but isn't a usable repo; start over.
		log.Warnf("template library cache is unusable (%v); re-cloning", err)
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return rmErr
		}
		_, cloneErr := git.PlainClone(dir, false, &git.CloneOptions{
			URL:           l.cfg.URL,
			ReferenceName: ref,
			SingleBranch:  true,
			Auth:          l.auth(),
		})
		return cloneErr
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	err = wt.Pull(&git.PullOptions{
		ReferenceName: ref,
		SingleBranch:  true,
		Force:         true,
		Auth:          l.auth(),
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

// ensureCloned clones on first use so callers don't have to call Sync first.
func (l *Library) ensureCloned() error {
	if _, err := os.Stat(filepath.Join(l.cachePath(), ".git")); err == nil {
		return nil
	}
	return l.sync()
}

// List returns the catalog's templates (metadata only), sorted by name.
func (l *Library) List() ([]Template, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureCloned(); err != nil {
		return nil, err
	}
	root, err := l.root()
	if err != nil {
		return nil, err
	}
	templates := []Template{}
	seen := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.IsDir() || d.Name() != "template.json" {
			return nil
		}
		dir := filepath.Dir(path)
		t, perr := parseManifest(dir, path, root)
		if perr != nil {
			log.Warnf("skipping library template at %s: %v", dir, perr)
			return nil
		}
		if seen[t.Name] {
			log.Warnf("duplicate library template name %q at %s; ignoring", t.Name, dir)
			return nil
		}
		seen[t.Name] = true
		templates = append(templates, t)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].Name < templates[j].Name })
	return templates, nil
}

// Get returns a single template by name with its bodies and attachments loaded.
func (l *Library) Get(name string) (Template, error) {
	list, err := l.List()
	if err != nil {
		return Template{}, err
	}
	for _, t := range list {
		if t.Name == name {
			return l.load(t)
		}
	}
	return Template{}, ErrNotFound
}

// parseManifest reads template.json and records which body files exist, without
// reading their contents.
func parseManifest(dir, manifestPath, root string) (Template, error) {
	raw, err := readCapped(manifestPath)
	if err != nil {
		return Template{}, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Template{}, fmt.Errorf("invalid template.json: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return Template{}, errors.New("template.json has no name")
	}
	category := filepath.ToSlash(relDir(root, dir))
	t := Template{
		Name:           m.Name,
		Subject:        m.Subject,
		EnvelopeSender: m.EnvelopeSender,
		Description:    m.Description,
		Category:       category,
		dir:            dir,
	}
	if fileExists(filepath.Join(dir, "html.html")) {
		t.HasHTML = true
	}
	if fileExists(filepath.Join(dir, "text.txt")) {
		t.HasText = true
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "attachments")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				t.AttachmentCount++
			}
		}
	}
	return t, nil
}

// load reads the body files and attachments for a template.
func (l *Library) load(t Template) (Template, error) {
	if t.HasHTML {
		b, err := readCapped(filepath.Join(t.dir, "html.html"))
		if err != nil {
			return Template{}, err
		}
		t.HTML = string(b)
	}
	if t.HasText {
		b, err := readCapped(filepath.Join(t.dir, "text.txt"))
		if err != nil {
			return Template{}, err
		}
		t.Text = string(b)
	}
	attachDir := filepath.Join(t.dir, "attachments")
	entries, err := os.ReadDir(attachDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			content, rerr := readCapped(filepath.Join(attachDir, e.Name()))
			if rerr != nil {
				return Template{}, rerr
			}
			t.Attachments = append(t.Attachments, Attachment{
				Name:    e.Name(),
				Type:    detectType(e.Name()),
				Content: content,
			})
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Template{}, err
	}
	return t, nil
}

// readCapped reads a file, refusing anything larger than maxFileSize.
func readCapped(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is larger than the %d byte limit", filepath.Base(path), maxFileSize)
	}
	return os.ReadFile(path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// relDir returns dir relative to root, or "" if it is root itself.
func relDir(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// detectType maps a file extension to a MIME type for the attachment, falling
// back to a generic binary type.
func detectType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
