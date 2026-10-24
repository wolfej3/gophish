package models

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

// makeZipAttachment builds a .docx-style attachment whose zip holds the given
// entries (name -> contents).
func makeZipAttachment(t *testing.T, entries [][2]string) Attachment {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatalf("creating zip entry: %v", err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatalf("writing zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return Attachment{Name: "payload.docx", Content: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

func testPtx(t *testing.T) PhishingTemplateContext {
	ptx, err := NewPhishingTemplateContext(
		ValidationContext{FromAddress: "a@b.com", BaseURL: "http://e.com"},
		BaseRecipient{Email: "a@b.com", FirstName: "A", LastName: "B"}, "rid")
	if err != nil {
		t.Fatalf("building context: %v", err)
	}
	return ptx
}

func TestAttachmentNormalDocxTemplates(t *testing.T) {
	a := makeZipAttachment(t, [][2]string{
		{"word/document.xml", "<xml>Hello {{.FirstName}}</xml>"},
		{"[Content_Types].xml", "<types/>"},
	})
	r, err := a.ApplyTemplate(testPtx(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, _ := io.ReadAll(r)
	// It should still be a readable zip
	if _, err := zip.NewReader(bytes.NewReader(out), int64(len(out))); err != nil {
		t.Fatalf("output isn't a valid zip: %v", err)
	}
	// And the template variable should have been applied somewhere inside
	if !bytes.Contains(out, []byte("Hello A")) && !strings.Contains(string(out), "Hello A") {
		// zip is compressed, so decode to check
		zr, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
		found := false
		for _, f := range zr.File {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			if bytes.Contains(b, []byte("Hello A")) {
				found = true
			}
		}
		if !found {
			t.Fatal("template variable wasn't applied")
		}
	}
}

func TestAttachmentZipTooManyEntries(t *testing.T) {
	old := maxZipEntries
	maxZipEntries = 2
	defer func() { maxZipEntries = old }()
	a := makeZipAttachment(t, [][2]string{
		{"a.xml", "x"}, {"b.xml", "y"}, {"c.xml", "z"},
	})
	if _, err := a.ApplyTemplate(testPtx(t)); err != ErrAttachmentTooLarge {
		t.Fatalf("expected ErrAttachmentTooLarge, got %v", err)
	}
}

func TestAttachmentZipEntryTooLarge(t *testing.T) {
	old := maxZipEntrySize
	maxZipEntrySize = 1024
	defer func() { maxZipEntrySize = old }()
	// A single highly compressible, oversized entry (the zip-bomb shape).
	a := makeZipAttachment(t, [][2]string{
		{"word/document.xml", strings.Repeat("A", 4096)},
	})
	if _, err := a.ApplyTemplate(testPtx(t)); err != ErrAttachmentTooLarge {
		t.Fatalf("expected ErrAttachmentTooLarge, got %v", err)
	}
}

func TestAttachmentZipTotalTooLarge(t *testing.T) {
	oldEntry, oldTotal := maxZipEntrySize, maxZipTotalSize
	maxZipEntrySize = 4096
	maxZipTotalSize = 6000
	defer func() { maxZipEntrySize, maxZipTotalSize = oldEntry, oldTotal }()
	a := makeZipAttachment(t, [][2]string{
		{"a.xml", strings.Repeat("A", 3000)},
		{"b.xml", strings.Repeat("B", 3000)},
		{"c.xml", strings.Repeat("C", 3000)},
	})
	if _, err := a.ApplyTemplate(testPtx(t)); err != ErrAttachmentTooLarge {
		t.Fatalf("expected ErrAttachmentTooLarge, got %v", err)
	}
}

func TestAttachmentDecodedSizeCapped(t *testing.T) {
	old := maxAttachmentDecodedSize
	maxAttachmentDecodedSize = 10
	defer func() { maxAttachmentDecodedSize = old }()
	a := Attachment{Name: "big.txt", Content: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 100)))}
	if _, err := a.ApplyTemplate(testPtx(t)); err != ErrAttachmentTooLarge {
		t.Fatalf("expected ErrAttachmentTooLarge for oversized text, got %v", err)
	}
}
