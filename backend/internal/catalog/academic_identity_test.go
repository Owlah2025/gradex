package catalog

import "testing"

// D-093 §2 and §3 restated in the domain: a Course value cannot describe an
// identity the database would refuse. These are the two halves of the
// classification invariant, plus the coexistence guard that keeps an Academic
// Course out of the legacy vocabulary.

func TestCourseClassificationInvariants(t *testing.T) {
	institution := "11111111-1111-1111-1111-111111111111"
	subject := "22222222-2222-2222-2222-222222222222"
	base := func() Course {
		return Course{ID: "course", OwnerAccountID: "owner", Lifecycle: LifecycleDraft}
	}

	cases := []struct {
		name    string
		mutate  func(*Course)
		wantErr bool
	}{
		{
			name:   "legacy course with no academic identity is valid",
			mutate: func(c *Course) { c.ClassificationModel = ClassificationLegacyTaxonomy },
		},
		{
			name: "academic course with an institution and no subject is valid while drafting",
			mutate: func(c *Course) {
				c.ClassificationModel = ClassificationAcademicCatalog
				c.InstitutionID = &institution
			},
		},
		{
			name: "academic course with an institution and a subject is valid",
			mutate: func(c *Course) {
				c.ClassificationModel = ClassificationAcademicCatalog
				c.InstitutionID = &institution
				c.SubjectID = &subject
			},
		},
		{
			name:    "academic course without an institution is refused",
			mutate:  func(c *Course) { c.ClassificationModel = ClassificationAcademicCatalog },
			wantErr: true,
		},
		{
			name: "legacy course carrying an institution is refused",
			mutate: func(c *Course) {
				c.ClassificationModel = ClassificationLegacyTaxonomy
				c.InstitutionID = &institution
			},
			wantErr: true,
		},
		{
			name: "legacy course carrying a subject is refused",
			mutate: func(c *Course) {
				c.ClassificationModel = ClassificationLegacyTaxonomy
				c.SubjectID = &subject
			},
			wantErr: true,
		},
		{
			name:    "an unknown classification model is refused",
			mutate:  func(c *Course) { c.ClassificationModel = ClassificationModel("SOMETHING_ELSE") },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			course := base()
			tc.mutate(&course)
			err := course.ValidateInvariants()
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateInvariants() = nil, want an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateInvariants() = %v, want nil", err)
			}
		})
	}
}

func TestRejectLegacyTaxonomyOnAcademicCourse(t *testing.T) {
	term := "33333333-3333-3333-3333-333333333333"
	year := StudyYear("YEAR_1")
	academic := &CourseRow{ClassificationModel: string(ClassificationAcademicCatalog)}
	legacy := &CourseRow{ClassificationModel: string(ClassificationLegacyTaxonomy)}

	// A legacy Course keeps the legacy vocabulary. This is the half that must not
	// regress before T5.
	if err := rejectLegacyTaxonomyOnAcademicCourse(legacy, &term, &term, &year); err != nil {
		t.Fatalf("legacy course refused its own taxonomy: %v", err)
	}

	// A nil row is a caller predating D-093 and is treated as legacy, which is
	// the pre-T4 behaviour exactly.
	if err := rejectLegacyTaxonomyOnAcademicCourse(nil, &term, &term, &year); err != nil {
		t.Fatalf("nil course row must default to legacy handling: %v", err)
	}

	// An Academic Course refuses each legacy dimension independently, so a
	// partial write cannot slip through.
	for _, tc := range []struct {
		name           string
		major, subject *string
		year           *StudyYear
	}{
		{name: "major", major: &term},
		{name: "subject", subject: &term},
		{name: "study year", year: &year},
		{name: "all three", major: &term, subject: &term, year: &year},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := rejectLegacyTaxonomyOnAcademicCourse(academic, tc.major, tc.subject, tc.year); err != ErrLegacyTaxonomyOnAcademicCourse {
				t.Fatalf("got %v, want ErrLegacyTaxonomyOnAcademicCourse", err)
			}
		})
	}

	// And an edit that touches no legacy field is untouched by the guard, so
	// ordinary title and description edits keep working on the shared route.
	if err := rejectLegacyTaxonomyOnAcademicCourse(academic, nil, nil, nil); err != nil {
		t.Fatalf("non-taxonomy edit on an academic course refused: %v", err)
	}
}
