package models

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Limits on attachment processing, to stop a zip bomb or an oversized file
// from exhausting memory when an attachment is templated for each recipient.
// These are vars rather than consts so tests can lower them; in production
// they keep their documented values.
var (
	// maxAttachmentDecodedSize caps the base64-decoded bytes we read into
	// memory to template (the zip container, or a text attachment).
	maxAttachmentDecodedSize int64 = 200 << 20 // 200 MB
	// maxZipEntries caps the number of files inside an Office archive.
	maxZipEntries = 1000
	// maxZipEntrySize caps a single entry's uncompressed size.
	maxZipEntrySize int64 = 50 << 20 // 50 MB
	// maxZipTotalSize caps the combined uncompressed size of all entries.
	maxZipTotalSize int64 = 200 << 20 // 200 MB
)

// ErrAttachmentTooLarge is returned when an attachment, or something inside a
// zip-based attachment, is larger than the processing limits allow.
var ErrAttachmentTooLarge = errors.New("Attachment is too large to process")

// Attachment contains the fields and methods for
// an email attachment
type Attachment struct {
	Id          int64  `json:"-"`
	TemplateId  int64  `json:"-"`
	Content     string `json:"content"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	vanillaFile bool   // Vanilla file has no template variables
}

// Validate ensures that the provided attachment uses the supported template variables correctly.
func (a Attachment) Validate() error {
	vc := ValidationContext{
		FromAddress: "foo@bar.com",
		BaseURL:     "http://example.com",
	}
	td := Result{
		BaseRecipient: BaseRecipient{
			Email:     "foo@bar.com",
			FirstName: "Foo",
			LastName:  "Bar",
			Position:  "Test",
		},
		RId: "123456",
	}
	ptx, err := NewPhishingTemplateContext(vc, td.BaseRecipient, td.RId)
	if err != nil {
		return err
	}
	_, err = a.ApplyTemplate(ptx)
	return err
}

// readCapped reads up to limit bytes from r, returning ErrAttachmentTooLarge
// if r holds more.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrAttachmentTooLarge
	}
	return b, nil
}

// ApplyTemplate parses different attachment files and applies the supplied phishing template.
func (a *Attachment) ApplyTemplate(ptx PhishingTemplateContext) (io.Reader, error) {

	decodedAttachment := base64.NewDecoder(base64.StdEncoding, strings.NewReader(a.Content))

	// If we've already determined there are no template variables in this attachment return it immediately
	if a.vanillaFile == true {
		return decodedAttachment, nil
	}

	// Decided to use the file extension rather than the content type, as there seems to be quite
	//  a bit of variability with types. e.g sometimes a Word docx file would have:
	//   "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	fileExtension := filepath.Ext(a.Name)

	switch fileExtension {

	case ".docx", ".docm", ".pptx", ".xlsx", ".xlsm":
		// Most modern office formats are xml based and can be unarchived.
		// .docm and .xlsm files are comprised of xml, and a binary blob for the macro code

		// Zip archives require random access for reading, so we buffer the
		// decoded bytes, capped so a huge upload can't exhaust memory.
		raw, err := readCapped(decodedAttachment, maxAttachmentDecodedSize)
		if err != nil {
			return nil, err
		}
		zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		if len(zipReader.File) > maxZipEntries {
			return nil, ErrAttachmentTooLarge
		}

		newZipArchive := new(bytes.Buffer)
		zipWriter := zip.NewWriter(newZipArchive) // For writing the new archive

		// i. Read each file from the Word document archive
		// ii. Apply the template to it
		// iii. Add the templated content to a new zip Word archive
		a.vanillaFile = true
		var totalSize int64
		for _, zipFile := range zipReader.File {
			// Reject early if the header already claims an oversized entry,
			// then enforce the real size while reading (headers can lie).
			if zipFile.UncompressedSize64 > uint64(maxZipEntrySize) {
				zipWriter.Close()
				return nil, ErrAttachmentTooLarge
			}
			contents, err := readZipEntry(zipFile)
			if err != nil {
				zipWriter.Close()
				return nil, err
			}
			totalSize += int64(len(contents))
			if totalSize > maxZipTotalSize {
				zipWriter.Close()
				return nil, ErrAttachmentTooLarge
			}
			subFileExtension := filepath.Ext(zipFile.Name)
			var tFile string
			if subFileExtension == ".xml" || subFileExtension == ".rels" { // Ignore other files, e.g binary ones and images
				// First we look for instances where Word has URL escaped our template variables. This seems to happen when inserting a remote image, converting {{.Foo}} to %7b%7b.foo%7d%7d.
				// See https://stackoverflow.com/questions/68287630/disable-url-encoding-for-includepicture-in-microsoft-word
				rx, _ := regexp.Compile("%7b%7b.([a-zA-Z]+)%7d%7d")
				contents := rx.ReplaceAllFunc(contents, func(m []byte) []byte {
					d, err := url.QueryUnescape(string(m))
					if err != nil {
						return m
					}
					return []byte(d)
				})

				// For each file apply the template.
				tFile, err = ExecuteTemplate(string(contents), ptx)
				if err != nil {
					zipWriter.Close() // Don't use defer when writing files https://www.joeshaw.org/dont-defer-close-on-writable-files/
					return nil, err
				}
				// Check if the subfile changed. We only need this to be set once to know in the future to check the 'parent' file
				if tFile != string(contents) {
					a.vanillaFile = false
				}
			} else {
				tFile = string(contents) // Could move this to the declaration of tFile, but might be confusing to read
			}
			// Write new Word archive
			newZipFile, err := zipWriter.Create(zipFile.Name)
			if err != nil {
				zipWriter.Close() // Don't use defer when writing files https://www.joeshaw.org/dont-defer-close-on-writable-files/
				return nil, err
			}
			_, err = newZipFile.Write([]byte(tFile))
			if err != nil {
				zipWriter.Close()
				return nil, err
			}
		}
		if err := zipWriter.Close(); err != nil {
			return nil, err
		}
		return bytes.NewReader(newZipArchive.Bytes()), nil

	case ".txt", ".html", ".ics":
		b, err := readCapped(decodedAttachment, maxAttachmentDecodedSize)
		if err != nil {
			return nil, err
		}
		processedAttachment, err := ExecuteTemplate(string(b), ptx)
		if err != nil {
			return nil, err
		}
		if processedAttachment == string(b) {
			a.vanillaFile = true
		}
		return strings.NewReader(processedAttachment), nil
	default:
		return decodedAttachment, nil // Default is to simply return the file
	}

}

// readZipEntry opens a zip entry, reads it with the per-entry cap enforced,
// and closes it, so entries aren't left open for the life of the loop.
func readZipEntry(zipFile *zip.File) ([]byte, error) {
	ff, err := zipFile.Open()
	if err != nil {
		return nil, err
	}
	defer ff.Close()
	return readCapped(ff, maxZipEntrySize)
}
