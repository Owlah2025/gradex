package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// ContentTypeDOCX is the OOXML WordprocessingML document media type. It is the
// only Word format Gradex accepts: the macro-enabled sibling
// (`application/vnd.ms-word.document.macroEnabled.12`) and the legacy binary
// `application/msword` are deliberately absent everywhere.
const ContentTypeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// The parts an OOXML WordprocessingML package must and must not contain. A
// declared media type, a file extension, and a bare ZIP signature are all
// caller-controlled, so none of them is evidence; these are read from the
// stored bytes.
const (
	ooxmlContentTypesPart      = "[Content_Types].xml"
	wordMainDocumentPart       = "word/document.xml"
	wordMainDocumentMediaType  = ContentTypeDOCX + ".main+xml"
	wordTemplateMediaType      = "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"
	wordMacroMainMediaTypeFrag = "macroenabled"
)

// vbaParts are the WordprocessingML parts that carry executable VBA. Their
// presence means the package is macro-bearing whatever its declared type says.
var vbaParts = map[string]struct{}{
	"word/vbaproject.bin": {},
	"word/vbadata.xml":    {},
	"vbaproject.bin":      {},
}

// ArchiveLimits bounds how much work an untrusted archive may cost us. Gradex
// never executes Office content; it only reads enough structure to prove the
// package really is a DOCX, and these bounds cap that read.
type ArchiveLimits struct {
	// MaxEntries bounds the member count, so a package of millions of tiny
	// entries cannot exhaust the inspection.
	MaxEntries int
	// MaxUncompressedBytes bounds the declared total expansion of the package.
	MaxUncompressedBytes int64
	// MaxCompressionRatio bounds expansion relative to the stored object, which
	// is the shape a ZIP bomb takes when each individual entry looks ordinary.
	MaxCompressionRatio int64
	// MaxPartBytes bounds a single part we actually decompress and parse.
	MaxPartBytes int64
}

// defaultArchiveLimits are generous for real course material and hostile to a
// decompression bomb. A 50 MB Lesson Resource of ordinary DOCX content sits
// orders of magnitude below every bound.
func defaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxEntries:           2048,
		MaxUncompressedBytes: 512 * 1024 * 1024,
		MaxCompressionRatio:  200,
		MaxPartBytes:         4 * 1024 * 1024,
	}
}

var errArchiveStructure = errors.New("archive structure is not an accepted DOCX package")

// validateDOCXObject proves the exact stored bytes are a macro-free OOXML
// WordprocessingML document rather than an arbitrary ZIP that was renamed or
// mis-declared. It parses structure only; nothing in the package is executed,
// rendered, or written to disk.
func validateDOCXObject(object []byte, limits ArchiveLimits) error {
	if int64(len(object)) <= 0 {
		return fmt.Errorf("%w: the object is empty", errArchiveStructure)
	}
	reader, err := zip.NewReader(bytes.NewReader(object), int64(len(object)))
	if err != nil {
		return fmt.Errorf("%w: the object is not a readable ZIP package: %v", errArchiveStructure, err)
	}
	if len(reader.File) == 0 {
		return fmt.Errorf("%w: the package has no entries", errArchiveStructure)
	}
	if limits.MaxEntries > 0 && len(reader.File) > limits.MaxEntries {
		return fmt.Errorf("%w: the package declares %d entries, above the %d bound",
			errArchiveStructure, len(reader.File), limits.MaxEntries)
	}

	var uncompressed int64
	var contentTypes *zip.File
	seenMainDocument := false
	for _, entry := range reader.File {
		name := strings.ToLower(entry.Name)
		if err := checkArchiveEntry(entry, name); err != nil {
			return err
		}
		uncompressed += int64(entry.UncompressedSize64)
		if limits.MaxUncompressedBytes > 0 && uncompressed > limits.MaxUncompressedBytes {
			return fmt.Errorf("%w: the package expands beyond the %d byte bound",
				errArchiveStructure, limits.MaxUncompressedBytes)
		}
		switch name {
		case strings.ToLower(ooxmlContentTypesPart):
			contentTypes = entry
		case wordMainDocumentPart:
			seenMainDocument = true
		}
	}
	if limits.MaxCompressionRatio > 0 && uncompressed > int64(len(object))*limits.MaxCompressionRatio {
		return fmt.Errorf("%w: the package expands %dx beyond its stored size, above the %dx bound",
			errArchiveStructure, uncompressed/int64(len(object)), limits.MaxCompressionRatio)
	}
	if contentTypes == nil {
		return fmt.Errorf("%w: %s is missing", errArchiveStructure, ooxmlContentTypesPart)
	}
	if !seenMainDocument {
		return fmt.Errorf("%w: %s is missing", errArchiveStructure, wordMainDocumentPart)
	}
	return checkWordContentTypes(contentTypes, limits.MaxPartBytes)
}

// checkArchiveEntry refuses the archive shapes that are dangerous regardless of
// what the package claims to be: escaping names, executable macro parts, and
// compression or encryption Gradex does not support.
func checkArchiveEntry(entry *zip.File, lowerName string) error {
	if _, macro := vbaParts[lowerName]; macro {
		return fmt.Errorf("%w: the package carries the macro part %q", errArchiveStructure, entry.Name)
	}
	normalized := strings.ReplaceAll(entry.Name, `\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "../") ||
		normalized == ".." || strings.HasPrefix(normalized, "../") {
		return fmt.Errorf("%w: the entry name %q escapes the package", errArchiveStructure, entry.Name)
	}
	if len(entry.Name) > 1 && entry.Name[1] == ':' {
		return fmt.Errorf("%w: the entry name %q is an absolute path", errArchiveStructure, entry.Name)
	}
	// Bit 0 of the general-purpose flag marks an encrypted entry, which cannot
	// be inspected and therefore cannot be proven safe.
	if entry.Flags&0x1 != 0 {
		return fmt.Errorf("%w: the entry %q is encrypted", errArchiveStructure, entry.Name)
	}
	if entry.Method != zip.Store && entry.Method != zip.Deflate {
		return fmt.Errorf("%w: the entry %q uses unsupported compression method %d",
			errArchiveStructure, entry.Name, entry.Method)
	}
	return nil
}

// checkWordContentTypes reads the package's own manifest and requires it to
// declare an ordinary WordprocessingML main document. A macro-enabled manifest
// is refused by name rather than inferred from an extension.
func checkWordContentTypes(part *zip.File, maxPartBytes int64) error {
	if maxPartBytes > 0 && int64(part.UncompressedSize64) > maxPartBytes {
		return fmt.Errorf("%w: %s is %d bytes, above the %d byte inspection bound",
			errArchiveStructure, ooxmlContentTypesPart, part.UncompressedSize64, maxPartBytes)
	}
	opened, err := part.Open()
	if err != nil {
		return fmt.Errorf("%w: %s could not be read: %v", errArchiveStructure, ooxmlContentTypesPart, err)
	}
	defer opened.Close()
	body, err := io.ReadAll(io.LimitReader(opened, maxPartBytes))
	if err != nil {
		return fmt.Errorf("%w: %s could not be read: %v", errArchiveStructure, ooxmlContentTypesPart, err)
	}

	var manifest struct {
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("%w: %s is not parseable XML: %v", errArchiveStructure, ooxmlContentTypesPart, err)
	}
	declared := false
	for _, override := range manifest.Overrides {
		contentType := strings.ToLower(strings.TrimSpace(override.ContentType))
		if strings.Contains(contentType, wordMacroMainMediaTypeFrag) {
			return fmt.Errorf("%w: the package declares the macro-enabled type %q",
				errArchiveStructure, override.ContentType)
		}
		if strings.EqualFold(strings.TrimSpace(override.PartName), "/"+wordMainDocumentPart) &&
			(contentType == wordMainDocumentMediaType || contentType == wordTemplateMediaType) {
			declared = true
		}
	}
	if !declared {
		return fmt.Errorf("%w: %s does not declare %s as a WordprocessingML main document",
			errArchiveStructure, ooxmlContentTypesPart, wordMainDocumentPart)
	}
	return nil
}

// contentMatchesDeclaredType is the bounded prefix probe run against the first
// bytes of the stored object. For DOCX it can only confirm the ZIP local file
// header; the structural proof needs the whole object and runs in
// validateDOCXObject.
func contentMatchesDeclaredType(prefix []byte, declared string) bool {
	if len(prefix) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(declared)) {
	case "video/mp4":
		return hasMP4FileTypeBox(prefix)
	case "application/pdf":
		return hasPDFHeader(prefix)
	case ContentTypeDOCX:
		return hasZipLocalFileHeader(prefix)
	default:
		return strings.EqualFold(mimetype.Detect(prefix).String(), strings.TrimSpace(declared))
	}
}

// hasMP4FileTypeBox accepts only a bounded ISO-BMFF file-type signature for
// video/mp4. A client declaration, extension, or generic octet-stream probe
// is not evidence that arbitrary bytes are an MP4 file.
func hasMP4FileTypeBox(prefix []byte) bool {
	if len(prefix) < 16 || binary.BigEndian.Uint32(prefix[:4]) < 16 || string(prefix[4:8]) != "ftyp" {
		return false
	}
	switch string(prefix[8:12]) {
	case "isom", "iso2", "iso4", "iso5", "iso6", "avc1", "mp41", "mp42", "dash":
		return true
	default:
		return false
	}
}

// hasPDFHeader requires the PDF signature at the very start of the object,
// tolerating only leading whitespace. Bytes that merely contain "%PDF-"
// somewhere are not a PDF.
func hasPDFHeader(prefix []byte) bool {
	trimmed := bytes.TrimLeft(prefix, " \t\r\n\v\f")
	return bytes.HasPrefix(trimmed, []byte("%PDF-"))
}

// hasZipLocalFileHeader recognises the ZIP local file header the OOXML package
// must begin with. It proves nothing about the package's contents on its own.
func hasZipLocalFileHeader(prefix []byte) bool {
	return bytes.HasPrefix(prefix, []byte{'P', 'K', 0x03, 0x04})
}
