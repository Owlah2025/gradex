package media

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// LZMA. Any method other than Store or Deflate is refused; this one stands in
// for the whole class.
const unsupportedZipMethod uint16 = 14

const wordMainDocumentContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"

const macroMainDocumentContentType = "application/vnd.ms-word.document.macroEnabled.main+xml"

type zipEntry struct {
	name   string
	body   string
	method uint16
	raw    []byte
}

// unsupportedCompressor lets a test emit an entry that declares a compression
// method Gradex does not support. archive/zip refuses to write one otherwise,
// and the refusal under test happens before any entry is opened.
type unsupportedCompressor struct{ io.Writer }

func (unsupportedCompressor) Close() error { return nil }

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	writer.RegisterCompressor(unsupportedZipMethod, func(w io.Writer) (io.WriteCloser, error) {
		return unsupportedCompressor{Writer: w}, nil
	})
	for _, entry := range entries {
		method := entry.method
		if method == 0 && entry.body != "" {
			method = zip.Deflate
		}
		w, err := writer.CreateHeader(&zip.FileHeader{Name: entry.name, Method: method})
		if err != nil {
			t.Fatalf("creating zip entry %q: %v", entry.name, err)
		}
		body := []byte(entry.body)
		if entry.raw != nil {
			body = entry.raw
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("writing zip entry %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return buffer.Bytes()
}

func contentTypesXML(mainType string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Override PartName="/word/document.xml" ContentType="` + mainType + `"/>` +
		`</Types>`
}

func minimalDOCX(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, []zipEntry{
		{name: "[Content_Types].xml", body: contentTypesXML(wordMainDocumentContentType)},
		{name: "_rels/.rels", body: `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{name: "word/document.xml", body: `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`},
	})
}

func TestValidDOCXIsAccepted(t *testing.T) {
	docx := minimalDOCX(t)
	if err := validateDOCXObject(docx, defaultArchiveLimits()); err != nil {
		t.Fatalf("a well-formed DOCX was rejected: %v", err)
	}
	// The bounded prefix probe must also accept the package's local file
	// header, because the structural proof needs the whole object.
	if !contentMatchesDeclaredType(docx[:32], ContentTypeDOCX) {
		t.Fatal("a DOCX local file header was rejected by the prefix probe")
	}
}

func TestArbitraryZipRenamedDOCXIsRejected(t *testing.T) {
	arbitrary := buildZip(t, []zipEntry{
		{name: "notes.txt", body: "this is not an Office package"},
		{name: "photo.bin", body: strings.Repeat("x", 64)},
	})
	err := validateDOCXObject(arbitrary, defaultArchiveLimits())
	if err == nil {
		t.Fatal("an arbitrary ZIP declared as DOCX was accepted")
	}
	// A generic ZIP local file header alone passes the cheap prefix probe; the
	// full structural check is what refuses it, and it must actually run.
	if !contentMatchesDeclaredType(arbitrary[:32], ContentTypeDOCX) {
		t.Fatal("prefix probe rejected the ZIP header, hiding the structural check")
	}
}

func TestMacroEnabledOfficeDocumentIsRejected(t *testing.T) {
	cases := map[string][]zipEntry{
		"macro main content type": {
			{name: "[Content_Types].xml", body: contentTypesXML(macroMainDocumentContentType)},
			{name: "word/document.xml", body: "<w:document/>"},
		},
		"vba project part": {
			{name: "[Content_Types].xml", body: contentTypesXML(wordMainDocumentContentType)},
			{name: "word/document.xml", body: "<w:document/>"},
			{name: "word/vbaProject.bin", body: "\x00\x01macro"},
		},
		"vba data part": {
			{name: "[Content_Types].xml", body: contentTypesXML(wordMainDocumentContentType)},
			{name: "word/document.xml", body: "<w:document/>"},
			{name: "word/vbaData.xml", body: "<wne:vbaSuppData/>"},
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateDOCXObject(buildZip(t, entries), defaultArchiveLimits()); err == nil {
				t.Fatal("macro-enabled Office content was accepted")
			}
		})
	}
}

func TestDangerousArchiveStructureIsRejected(t *testing.T) {
	base := []zipEntry{
		{name: "[Content_Types].xml", body: contentTypesXML(wordMainDocumentContentType)},
		{name: "word/document.xml", body: "<w:document/>"},
	}
	cases := map[string]zipEntry{
		"parent traversal":   {name: "../../etc/passwd", body: "root"},
		"absolute path":      {name: "/etc/shadow", body: "root"},
		"windows drive path": {name: `C:\windows\system32\x.dll`, body: "mz"},
		"backslash escape":   {name: `..\..\windows\x.dll`, body: "mz"},
	}
	for name, hostile := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateDOCXObject(buildZip(t, append(append([]zipEntry{}, base...), hostile)), defaultArchiveLimits()); err == nil {
				t.Fatal("a hostile archive path was accepted")
			}
		})
	}

	t.Run("unsupported compression method", func(t *testing.T) {
		entries := append(append([]zipEntry{}, base...), zipEntry{name: "word/extra.bin", body: "x", method: unsupportedZipMethod})
		if err := validateDOCXObject(buildZip(t, entries), defaultArchiveLimits()); err == nil {
			t.Fatal("an unsupported ZIP compression method was accepted")
		}
	})
}

func TestArchiveExhaustionBoundsAreEnforced(t *testing.T) {
	base := []zipEntry{
		{name: "[Content_Types].xml", body: contentTypesXML(wordMainDocumentContentType)},
		{name: "word/document.xml", body: "<w:document/>"},
	}

	t.Run("entry count", func(t *testing.T) {
		limits := defaultArchiveLimits()
		limits.MaxEntries = 4
		entries := append([]zipEntry{}, base...)
		for i := range 8 {
			entries = append(entries, zipEntry{name: "word/media/image" + string(rune('a'+i)) + ".bin", body: "x"})
		}
		if err := validateDOCXObject(buildZip(t, entries), limits); err == nil {
			t.Fatal("an archive above the entry-count bound was accepted")
		}
	})

	t.Run("aggregate uncompressed size", func(t *testing.T) {
		limits := defaultArchiveLimits()
		limits.MaxUncompressedBytes = 1024
		entries := append(append([]zipEntry{}, base...),
			zipEntry{name: "word/media/big.bin", body: strings.Repeat("A", 64*1024)})
		if err := validateDOCXObject(buildZip(t, entries), limits); err == nil {
			t.Fatal("an archive above the aggregate uncompressed bound was accepted")
		}
	})

	t.Run("compression ratio", func(t *testing.T) {
		limits := defaultArchiveLimits()
		limits.MaxCompressionRatio = 4
		// Highly compressible filler: small stored bytes, large expansion.
		entries := append(append([]zipEntry{}, base...),
			zipEntry{name: "word/media/bomb.bin", body: strings.Repeat("A", 512*1024)})
		if err := validateDOCXObject(buildZip(t, entries), limits); err == nil {
			t.Fatal("an archive above the compression-ratio bound was accepted")
		}
	})
}

func TestNonArchiveBytesDeclaredAsDOCXAreRejected(t *testing.T) {
	for name, body := range map[string][]byte{
		"empty":     {},
		"plaintext": []byte("Dear reviewer, this is not a package."),
		"pdf":       []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"),
		"truncated": minimalDOCX(t)[:16],
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDOCXObject(body, defaultArchiveLimits()); err == nil {
				t.Fatal("non-DOCX bytes were accepted as a DOCX")
			}
		})
	}
	if contentMatchesDeclaredType([]byte("%PDF-1.7\n"), ContentTypeDOCX) {
		t.Fatal("PDF bytes passed the DOCX prefix probe")
	}
}

func TestPDFValidationUsesActualBytes(t *testing.T) {
	if !contentMatchesDeclaredType([]byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"), "application/pdf") {
		t.Fatal("a real PDF header was rejected")
	}
	for name, body := range map[string][]byte{
		"empty":          {},
		"html":           []byte("<!doctype html><html><body>not a pdf</body></html>"),
		"docx":           minimalDOCX(t),
		"late signature": append(bytes.Repeat([]byte{'x'}, 2048), []byte("%PDF-1.7")...),
	} {
		t.Run(name, func(t *testing.T) {
			if contentMatchesDeclaredType(body, "application/pdf") {
				t.Fatal("non-PDF bytes were accepted as a PDF")
			}
		})
	}
}
