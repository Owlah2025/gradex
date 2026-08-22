//go:build integration

package catalog

import "testing"

func TestT4EAcademicAndLegacyReviewProjections(t *testing.T) {
	repo, _, ctx, academicCourse := setupT4C(t)
	academicReview, err := repo.GetCourseRevisionGraph(ctx, academicCourse.ID, academicCourse.EditableRevision.ID)
	if err != nil {
		t.Fatalf("loading Academic review projection: %v", err)
	}
	if academicReview.ClassificationModel != ClassificationAcademicCatalog || academicReview.AcademicContext == nil {
		t.Fatalf("Academic review classification/context = %s/%#v",
			academicReview.ClassificationModel, academicReview.AcademicContext)
	}
	if academicReview.AcademicContext.InstitutionNameEn != "Kuwait University" ||
		academicReview.AcademicContext.Subject == nil ||
		academicReview.AcademicContext.Subject.OfficialCode == nil ||
		*academicReview.AcademicContext.Subject.OfficialCode != "0418-320" {
		t.Fatalf("Academic review context = %#v", academicReview.AcademicContext)
	}
	if academicReview.EditableRevision.Audience == nil ||
		academicReview.EditableRevision.Audience.Mode != AudienceAutomatic ||
		len(academicReview.EditableRevision.Audience.Programs) != 2 {
		t.Fatalf("Academic review audience = %#v", academicReview.EditableRevision.Audience)
	}

	legacyCourse, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor, TitleAr: "قديم", TitleEn: "Legacy Review",
	}, "T4E Instructor")
	if err != nil {
		t.Fatalf("creating legacy review fixture: %v", err)
	}
	legacyReview, err := repo.GetCourseRevisionGraph(ctx, legacyCourse.ID, legacyCourse.EditableRevision.ID)
	if err != nil {
		t.Fatalf("loading legacy review projection: %v", err)
	}
	if legacyReview.ClassificationModel != ClassificationLegacyTaxonomy || legacyReview.AcademicContext != nil {
		t.Fatalf("legacy review classification/context = %s/%#v",
			legacyReview.ClassificationModel, legacyReview.AcademicContext)
	}
	if legacyReview.EditableRevision.Audience != nil {
		t.Fatalf("legacy revision exposes Academic audience: %#v", legacyReview.EditableRevision.Audience)
	}
}
