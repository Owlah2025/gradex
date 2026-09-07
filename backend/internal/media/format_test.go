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

// realFFmpegWebMPrefix is the first 48 bytes of a WebM file produced by
// ffmpeg (`ffmpeg -f lavfi -i testsrc -c:v libvpx out.webm`). Byte-for-byte it
// is the canonical EBML header: the EBML ID, a one-byte header size (0x9f),
// then EBMLVersion/EBMLReadVersion/EBMLMaxIDLength/EBMLMaxSizeLength, the
// DocType element `42 82 84 "webm"` -- size 0x84 because "webm" is four bytes
// long -- then DocTypeVersion/DocTypeReadVersion and the start of the Segment.
// libvpx (VP8) and libvpx-vp9 emit an identical header here.
var realFFmpegWebMPrefix = []byte{
	0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81, 0x01, 0x42, 0xf7, 0x81,
	0x01, 0x42, 0xf2, 0x81, 0x04, 0x42, 0xf3, 0x81, 0x08, 0x42, 0x82, 0x84,
	0x77, 0x65, 0x62, 0x6d, 0x42, 0x87, 0x81, 0x02, 0x42, 0x85, 0x81, 0x02,
	0x18, 0x53, 0x80, 0x67, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x07, 0xe1,
}

// realFFmpegMatroskaPrefix is the same probe against `-f matroska` output. It
// differs only in the DocType element: `42 82 88 "matroska"`.
var realFFmpegMatroskaPrefix = []byte{
	0x1a, 0x45, 0xdf, 0xa3, 0xa3, 0x42, 0x86, 0x81, 0x01, 0x42, 0xf7, 0x81,
	0x01, 0x42, 0xf2, 0x81, 0x04, 0x42, 0xf3, 0x81, 0x08, 0x42, 0x82, 0x88,
	0x6d, 0x61, 0x74, 0x72, 0x6f, 0x73, 0x6b, 0x61, 0x42, 0x87, 0x81, 0x04,
	0x42, 0x85, 0x81, 0x02, 0x18, 0x53, 0x80, 0x67, 0x01, 0x00, 0x00, 0x00,
}

// TestWebMDetectionMatchesRealEncoderOutput pins the detector to the DocType
// encoding real encoders emit. The previous implementation searched for the
// literal bytes `42 82 85 "webm"`, which no canonical four-byte DocType ever
// contains, so every genuine ffmpeg WebM fell through to generic validation.
func TestWebMDetectionMatchesRealEncoderOutput(t *testing.T) {
	if !hasWebMFileTypeHeader(realFFmpegWebMPrefix) {
		t.Fatal("real ffmpeg WebM output was not detected as WebM")
	}
	if got := recognizedVideoContentType(realFFmpegWebMPrefix); got != "video/webm" {
		t.Fatalf("real ffmpeg WebM output was recognized as %q, want video/webm", got)
	}
	if !contentTypeMismatch(realFFmpegWebMPrefix, "video/mp4") {
		t.Fatal("real ffmpeg WebM declared as MP4 was not a typed content-type mismatch")
	}
	if contentTypeMismatch(realFFmpegWebMPrefix, "video/webm") {
		t.Fatal("real ffmpeg WebM declared as WebM was classified as a mismatch")
	}
}

// TestWebMDetectionStaysConservative keeps the widened detection from turning
// neighbouring or unknown byte sequences into WebM content-type mismatches.
func TestWebMDetectionStaysConservative(t *testing.T) {
	// A bare EBML header carrying every sibling element except DocType.
	bareEBML := []byte{
		0x1a, 0x45, 0xdf, 0xa3, 0x94, 0x42, 0x86, 0x81, 0x01, 0x42, 0xf7, 0x81,
		0x01, 0x42, 0xf2, 0x81, 0x04, 0x42, 0xf3, 0x81, 0x08, 0x42, 0x87, 0x81,
		0x02, 0x42, 0x85, 0x81, 0x02,
	}
	// The DocType element bytes present, but not inside a parsable EBML header.
	docTypeInJunk := append([]byte{0x1a, 0x45, 0xdf, 0xa3, 0xff, 0xff, 0xff, 0xff},
		append([]byte{0x42, 0x82, 0x84}, []byte("webm")...)...)

	for name, body := range map[string][]byte{
		"bare EBML without DocType": bareEBML,
		"real ffmpeg Matroska":      realFFmpegMatroskaPrefix,
		"DocType bytes in junk":     docTypeInJunk,
		"arbitrary bytes":           {0x00, 0xff, 0x01, 0x7f, 0x80, 0xfe, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa},
		"truncated EBML magic":      {0x1a, 0x45, 0xdf},
		"MP4":                       append([]byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6f, 0x6d}, bytes.Repeat([]byte{0x00}, 8)...),
		"PDF":                       []byte("%PDF-1.7\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if hasWebMFileTypeHeader(body) {
				t.Fatal("non-WebM bytes were detected as WebM")
			}
			if contentTypeMismatch(body, "video/quicktime") && recognizedVideoContentType(body) == "video/webm" {
				t.Fatal("non-WebM bytes produced a typed WebM content-type mismatch")
			}
		})
	}
}

func TestRecognizedVideoContainerMismatchIsTypedWithoutOverclassifyingUnknownBytes(t *testing.T) {
	webm := realFFmpegWebMPrefix
	if !contentTypeMismatch(webm, "video/mp4") {
		t.Fatal("recognized WebM bytes were not classified as an MP4 content-type mismatch")
	}
	if contentTypeMismatch(webm, "video/webm") {
		t.Fatal("matching WebM content was classified as a mismatch")
	}
	if contentTypeMismatch([]byte{0x00, 0xff, 0x01, 0x7f, 0x80, 0xfe}, "video/mp4") {
		t.Fatal("unknown bytes were overclassified as a content-type mismatch")
	}
	if contentTypeMismatch([]byte("%PDF-1.7\n"), "video/mp4") {
		t.Fatal("non-video bytes were classified as a typed video mismatch")
	}
	if contentTypeMismatch(webm, "application/pdf") {
		t.Fatal("video bytes for a non-video declaration were overclassified")
	}
}
