package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	tests := map[string]int{
		// Assets referenced by the admin templates are served
		"/css/dist/gophish.css":               http.StatusOK,
		"/js/dist/vendor.min.js":              http.StatusOK,
		"/js/dist/app/gophish.min.js":         http.StatusOK,
		"/js/src/vendor/ckeditor/ckeditor.js": http.StatusOK,
		"/images/favicon.ico":                 http.StatusOK,
		"/images/pixel.png":                   http.StatusOK,
		// Directory contents aren't listed
		"/js/dist/": http.StatusForbidden,
		"/images/":  http.StatusForbidden,
		"/":         http.StatusForbidden,
		// Sources, user content, and the GeoIP database aren't embedded
		"/js/src/app/gophish.js": http.StatusNotFound,
		"/endpoint/":             http.StatusNotFound,
		"/db/geolite2-city.mmdb": http.StatusNotFound,
		"/../config.json":        http.StatusNotFound,
	}
	handler := Handler()
	for path, expected := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != expected {
			t.Errorf("%s: expected status %d, got %d", path, expected, w.Code)
		}
	}
}
