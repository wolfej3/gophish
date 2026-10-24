package api

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophish/gophish/models"
)

// TestTemplateRejectsZipBomb ensures a zip-based attachment that exceeds the
// processing limits is rejected at save time with a 400, not discovered
// during sending. It uses the entry-count cap so the test stays small.
func TestTemplateRejectsZipBomb(t *testing.T) {
	testCtx := setupTest(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < 1001; i++ {
		w, _ := zw.Create(fmt.Sprintf("f%d.xml", i))
		w.Write([]byte("x"))
	}
	zw.Close()
	tmpl := models.Template{
		Name: "Bomb",
		Text: "hi",
		Attachments: []models.Attachment{{
			Name:    "payload.docx",
			Content: base64.StdEncoding.EncodeToString(buf.Bytes()),
		}},
	}
	w := apiRequest(t, testCtx, http.MethodPost, "/api/templates/", testCtx.apiKey, tmpl)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a zip bomb attachment, got %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("too large")) {
		t.Fatalf("expected a too-large message, got %s", w.Body.String())
	}
}
