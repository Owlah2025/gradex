package learning

import (
	"errors"
	"testing"
)

// Request normalisation and the closed enumerations, proved without a database. Everything that
// depends on the authoritative graph is proved in report_integration_test.go against real
// PostgreSQL instead.

func TestReportTargetKindsAndReasonsAreClosedSets(t *testing.T) {
	for _, kind := range []ReportTargetKind{
		ReportTargetCourse, ReportTargetLesson, ReportTargetVideo, ReportTargetResource, ReportTargetLabMaterial,
	} {
		if !kind.valid() {
			t.Fatalf("target kind %q must be accepted", kind)
		}
	}
	// Anything outside the migration's CHECK is refused before it can reach the database.
	for _, kind := range []ReportTargetKind{"", "SECTION", "course", "OFFICE_HOURS", "PREVIEW"} {
		if kind.valid() {
			t.Fatalf("target kind %q must be refused", kind)
		}
	}

	for _, reason := range []ReportReason{
		ReasonBrokenUnavailable, ReasonInaccurate, ReasonInappropriate, ReasonSuspectedCopyrightViolatio, ReasonOther,
	} {
		if !reason.valid() {
			t.Fatalf("reason %q must be accepted", reason)
		}
	}
	for _, reason := range []ReportReason{"", "spam", "Other", "OTHER", "copyright"} {
		if reason.valid() {
			t.Fatalf("reason %q must be refused", reason)
		}
	}
}

func TestNormalizeReportContentRejectsUnusableInput(t *testing.T) {
	base := ReportContent{Reason: ReasonInaccurate}

	cases := []struct {
		name    string
		mutate  func(ReportContent) ReportContent
		wantErr error
	}{
		{"unknown reason", func(c ReportContent) ReportContent { c.Reason = "spam"; return c }, ErrReportInvalid},
		{
			"other without explanation",
			func(c ReportContent) ReportContent { c.Reason = ReasonOther; return c },
			ErrReportInvalid,
		},
		{
			"other with only whitespace",
			func(c ReportContent) ReportContent { c.Reason = ReasonOther; c.Explanation = " \t\n "; return c },
			ErrReportInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := normalizeReportContent(testCase.mutate(base)); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, err)
			}
		})
	}
}

// A binding is trusted data, but its *shape* is still checked: a Course or Lesson report carries no
// Asset Version, and a media report cannot be missing one.
func TestValidateBindingShapeEnforcesPerKindVersionPresence(t *testing.T) {
	valid := VerifiedReportBinding{
		ReporterAccountID:       "11111111-1111-1111-1111-111111111111",
		CourseID:                "22222222-2222-2222-2222-222222222222",
		TargetKind:              ReportTargetLesson,
		StableTargetID:          "33333333-3333-3333-3333-333333333333",
		VisibleCourseRevisionID: "55555555-5555-5555-5555-555555555555",
	}
	if err := validateBindingShape(valid); err != nil {
		t.Fatalf("a complete Lesson binding must be accepted: %v", err)
	}

	withVersion := valid
	withVersion.VisibleAssetVersionID = "66666666-6666-6666-6666-666666666666"
	if err := validateBindingShape(withVersion); !errors.Is(err, ErrReportInvalid) {
		t.Fatalf("a Lesson binding must carry no Asset Version, got %v", err)
	}

	video := valid
	video.TargetKind = ReportTargetVideo
	if err := validateBindingShape(video); !errors.Is(err, ErrReportInvalid) {
		t.Fatalf("a VIDEO binding must require an Asset Version, got %v", err)
	}

	incomplete := valid
	incomplete.VisibleCourseRevisionID = "  "
	if err := validateBindingShape(incomplete); !errors.Is(err, ErrReportInvalid) {
		t.Fatalf("an incomplete binding must be refused, got %v", err)
	}

	unknown := valid
	unknown.TargetKind = "SECTION"
	if err := validateBindingShape(unknown); !errors.Is(err, ErrReportInvalid) {
		t.Fatalf("an unknown kind must be refused, got %v", err)
	}
}

func TestNormalizeReportContentTrimsWithoutAlteringContent(t *testing.T) {
	normalized, err := normalizeReportContent(ReportContent{
		Reason:      ReasonOther,
		Explanation: "  الفيديو لا يعمل — the video stops at 0:12.  ",
	})
	if err != nil {
		t.Fatalf("normalising valid content: %v", err)
	}
	// Surrounding whitespace is not content; the Student's words, including non-Latin script and
	// punctuation, are stored exactly as written.
	if normalized.Explanation != "الفيديو لا يعمل — the video stops at 0:12." {
		t.Fatalf("explanation altered: %q", normalized.Explanation)
	}
}

func TestNormalizeReportContentKeepsAnExplanationForNonOtherReasons(t *testing.T) {
	// An explanation is required only for `other`; it is permitted, and preserved, for any reason.
	normalized, err := normalizeReportContent(ReportContent{
		Reason:      ReasonBrokenUnavailable,
		Explanation: " the PDF is empty ",
	})
	if err != nil {
		t.Fatalf("normalising: %v", err)
	}
	if normalized.Explanation != "the PDF is empty" {
		t.Fatalf("explanation not preserved: %q", normalized.Explanation)
	}
}

func TestCreateReportRequiresAClock(t *testing.T) {
	// The submission instant comes from an injected boundary, never from an implicit wall clock.
	repository := &Repository{}
	if _, err := repository.CreateReport(t.Context(), VerifiedReportBinding{}, ReportContent{}, nil); err == nil {
		t.Fatal("a report must not be created without a clock or a pool")
	}
}
