// Package static embeds the built assets served by the admin interface.
//
// User-provided content stays on disk and isn't embedded: files in
// static/endpoint are served by the phishing server, and the GeoIP database
// in static/db is meant to be updated independently of releases.
package static

import (
	"embed"
	"net/http"
	"os"
	"strings"
)

// FS contains the built CSS, fonts, images, and JavaScript for the admin
// interface, along with the CKEditor sources it loads directly.
//
//go:embed css/dist font images js/dist js/src/vendor/ckeditor
var FS embed.FS

// Handler returns an http.Handler serving the embedded assets without
// directory listings.
func Handler() http.Handler {
	return http.FileServer(unindexedFS{http.FS(FS)})
}

// unindexedFS is an http.FileSystem that refuses to open directories unless
// they contain an index.html, so that directory contents aren't listed.
type unindexedFS struct {
	fs http.FileSystem
}

// Open opens the named file. Opening a directory without an index.html
// returns os.ErrPermission, which http.FileServer serves as 403 Forbidden.
func (ufs unindexedFS) Open(name string) (http.File, error) {
	f, err := ufs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	s, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if s.IsDir() {
		index, err := ufs.fs.Open(strings.TrimSuffix(name, "/") + "/index.html")
		if err != nil {
			f.Close()
			return nil, os.ErrPermission
		}
		index.Close()
	}
	return f, nil
}
