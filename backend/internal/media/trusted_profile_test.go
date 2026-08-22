package media

import "testing"

// The D-088 allowlist is deliberately narrow. Anything outside it — including
// every other type the wider BR-067 resource bucket permits — must fail closed
// rather than take the no-malware-scan path.
func TestTrustedProfileAdmitsOnlyTheApprovedLessonMediaTypes(t *testing.T) {
	admitted := []struct {
		kind        AssetKind
		contentType string
	}{
		{KindVideo, "video/mp4"},
		{KindVideo, "VIDEO/MP4"},
		{KindResource, "application/pdf"},
		{KindResource, ContentTypeDOCX},
	}
	for _, tc := range admitted {
		if !TrustedProfileAdmits(tc.kind, tc.contentType) {
			t.Errorf("TrustedProfileAdmits(%q, %q) = false, want true", tc.kind, tc.contentType)
		}
	}

	refused := []struct {
		name        string
		kind        AssetKind
		contentType string
	}{
		{"public preview video", KindPreview, "video/mp4"},
		{"public preview pdf", KindPreview, "application/pdf"},
		{"lab material archive", KindLabMaterial, "application/zip"},
		{"lab material pdf", KindLabMaterial, "application/pdf"},
		{"quicktime video", KindVideo, "video/quicktime"},
		{"resource pdf-like video", KindVideo, "application/pdf"},
		{"resource image", KindResource, "image/png"},
		{"resource archive", KindResource, "application/zip"},
		{"macro-enabled word", KindResource, "application/vnd.ms-word.document.macroEnabled.12"},
		{"legacy word", KindResource, "application/msword"},
		{"unknown kind", AssetKind("OTHER"), "application/pdf"},
		{"empty type", KindResource, ""},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if TrustedProfileAdmits(tc.kind, tc.contentType) {
				t.Fatalf("TrustedProfileAdmits(%q, %q) = true, want false", tc.kind, tc.contentType)
			}
		})
	}
}

// A trusted Lesson Resource becomes READY straight from VALIDATED; a trusted
// Lesson video still owes the FFmpeg evidence and must go through PROCESSING.
func TestTrustedValidationRequiresProcessingOnlyForVideo(t *testing.T) {
	if !trustedRequiresProcessing(KindVideo) {
		t.Fatal("a trusted video skipped processing")
	}
	if trustedRequiresProcessing(KindResource) {
		t.Fatal("a trusted Lesson Resource was sent to processing")
	}
}

// One canonical D-088 profile identifier, written by this package and required
// by migration 0020's provenance check. The literal is pinned on both sides so
// renaming the Go constant cannot silently stop the database from recognising
// the evidence this package writes.
func TestTrustedValidationProfileIdentifierIsCanonical(t *testing.T) {
	if TrustedValidationProfile != "D-088-TRUSTED-INSTRUCTOR" {
		t.Fatalf("TrustedValidationProfile = %q; migration 0020 requires %q",
			TrustedValidationProfile, "D-088-TRUSTED-INSTRUCTOR")
	}
}

func TestOperatingModeValidatesTheThreeAuthorizedModes(t *testing.T) {
	for _, mode := range []OperatingMode{
		OperatingModeScanner, OperatingModeAdminCatalogue, OperatingModeTrustedInstructor,
	} {
		if !mode.Valid() {
			t.Errorf("%q is not valid", mode)
		}
	}
	for _, mode := range []OperatingMode{"", "BYPASS", "trusted_instructor", "TRUSTED"} {
		if mode.Valid() {
			t.Errorf("%q was accepted as an operating mode", mode)
		}
	}
}
