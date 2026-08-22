package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

type SubmissionViolation struct {
	Code      string `json:"code"`
	Target    string `json:"target"`
	Dimension string `json:"dimension,omitempty"`
}

type SubmissionValidationError struct {
	Violations []SubmissionViolation `json:"violations"`
}

type submissionValidationRequest struct {
	tx        pgx.Tx
	validator AssetVersionValidator
	courseID  string
	revision  *CourseRevision
	// course carries the locked Course row so the classification model is read
	// from committed state rather than from anything a caller supplied. It is
	// optional only so that pre-T4 callers that predate the field keep
	// compiling; a nil course is treated as LEGACY_TAXONOMY, which is the
	// pre-T4 behaviour exactly.
	course *CourseRow
}

func (e *SubmissionValidationError) Error() string {
	return fmt.Sprintf("course submission incomplete: %d violations", len(e.Violations))
}

// validateCourseForSubmission collects every graph and dependency violation in one pass.
func validateCourseForSubmission(
	ctx context.Context,
	req submissionValidationRequest,
) (*SubmissionValidationError, error) {
	if req.tx == nil {
		return nil, fmt.Errorf("submission validation transaction is required")
	}
	if req.validator == nil {
		return nil, fmt.Errorf("asset version validator is required")
	}

	var violations []SubmissionViolation
	var err error

	if req.revision == nil {
		violations = append(violations, SubmissionViolation{
			Code:   "COURSE_EMPTY",
			Target: "course:" + req.courseID,
		})
		return &SubmissionValidationError{Violations: violations}, nil
	}

	// 1. Classification-dimension validation.
	//
	// D-093 §6: the two models are validated by their own rules and never by
	// both. A LEGACY_TAXONOMY Course keeps FR-010 exactly as written, so every
	// existing Course, test, and E2E assertion is unaffected. An
	// ACADEMIC_CATALOG Course is held to canonical Subject requirements and is
	// never asked to populate legacy terms it must not own.
	//
	// This runs on the submission path and on the approval revalidation path,
	// because ApproveCourse calls this same function. One branch, both gates.
	if academicSubmissionModel(req.course) {
		violations = append(violations, validateAcademicIdentityForSubmission(ctx, req)...)
	} else {
		violations = append(violations, validateLegacyTaxonomyForSubmission(ctx, req, &err)...)
		if err != nil {
			return nil, err
		}
	}

	if req.revision.PreviewAssetVersionID != nil && *req.revision.PreviewAssetVersionID != "" {
		if err := validatePreviewAsset(ctx, req.tx, req.courseID, req.revision.ID, *req.revision.PreviewAssetVersionID, false); err != nil {
			violations = append(violations, SubmissionViolation{
				Code:      "ASSET_VERSION_UNAVAILABLE",
				Target:    "asset:" + *req.revision.PreviewAssetVersionID,
				Dimension: "PREVIEW",
			})
		}
	}

	// 2. Sections and Lessons completeness validation (FR-009)
	if len(req.revision.Sections) == 0 {
		violations = append(violations, SubmissionViolation{
			Code:   "COURSE_EMPTY",
			Target: "course:" + req.courseID,
		})
	} else {
		for _, sec := range req.revision.Sections {
			if len(sec.Lessons) == 0 {
				violations = append(violations, SubmissionViolation{
					Code:   "SECTION_EMPTY",
					Target: "section:" + sec.SectionIdentityID,
				})
			} else {
				for _, les := range sec.Lessons {
					if les.VideoAssetVersionID == nil || *les.VideoAssetVersionID == "" {
						violations = append(violations, SubmissionViolation{
							Code:   "LESSON_VIDEO_MISSING",
							Target: "lesson:" + les.LessonIdentityID,
						})
					} else {
						if err := req.validator.ValidateAssetVersion(ctx, *les.VideoAssetVersionID); err != nil {
							violations = append(violations, SubmissionViolation{
								Code:   "LESSON_VIDEO_MISSING",
								Target: "lesson:" + les.LessonIdentityID,
							})
						}
					}
					for _, file := range les.Files {
						if err := validateLessonFileDependency(ctx, req, les, file); err != nil {
							violations = append(violations, SubmissionViolation{
								Code:      "ASSET_VERSION_UNAVAILABLE",
								Target:    "file:" + file.ID,
								Dimension: string(file.Kind),
							})
						}
					}
				}
			}
		}
	}

	if len(violations) > 0 {
		return &SubmissionValidationError{Violations: violations}, nil
	}

	return nil, nil
}

// academicSubmissionModel reports whether the Academic Catalog rules apply. A
// nil Course row means a caller that predates D-093, which is LEGACY_TAXONOMY.
func academicSubmissionModel(course *CourseRow) bool {
	return course != nil && ClassificationModel(course.ClassificationModel) == ClassificationAcademicCatalog
}

// validateLegacyTaxonomyForSubmission is D-022's FR-010 gate, unchanged. It is
// authoritative for every Course until T5 migrates it.
func validateLegacyTaxonomyForSubmission(
	ctx context.Context,
	req submissionValidationRequest,
	fatal *error,
) []SubmissionViolation {
	var violations []SubmissionViolation

	if req.revision.MajorTermID == nil || *req.revision.MajorTermID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: string(TaxonomyMajor),
		})
	} else if unavailable, err := taxonomyUnavailable(ctx, req.tx, *req.revision.MajorTermID, TaxonomyMajor); err != nil {
		*fatal = err
		return violations
	} else if unavailable {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_TERM_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: string(TaxonomyMajor),
		})
	}
	if req.revision.SubjectTermID == nil || *req.revision.SubjectTermID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: string(TaxonomySubject),
		})
	} else if unavailable, err := taxonomyUnavailable(ctx, req.tx, *req.revision.SubjectTermID, TaxonomySubject); err != nil {
		*fatal = err
		return violations
	} else if unavailable {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_TERM_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: string(TaxonomySubject),
		})
	}
	if req.revision.StudyYear == nil {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: "STUDY_YEAR",
		})
	}
	return violations
}

// validateAcademicIdentityForSubmission is the D-093 gate. It deliberately does
// NOT read major_term_id, subject_term_id, or study_year: an Academic Course
// must never be pushed into populating legacy terms to pass review.
//
// Program targets are revision-scoped. Zero rows is automatic inference and
// remains valid even when inference is empty; explicit rows must stay a subset
// of the Subject's current eligible Program mappings.
func validateAcademicIdentityForSubmission(
	ctx context.Context,
	req submissionValidationRequest,
) []SubmissionViolation {
	var violations []SubmissionViolation

	if req.course.InstitutionID == nil || *req.course.InstitutionID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "ACADEMIC_INSTITUTION_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: "INSTITUTION",
		})
		return violations
	}

	if req.course.SubjectID == nil || *req.course.SubjectID == "" {
		// Covers both the Instructor still searching and a Subject request that
		// has not been resolved. Drafting is unaffected; only submission stops.
		violations = append(violations, SubmissionViolation{
			Code:      "ACADEMIC_SUBJECT_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: "SUBJECT",
		})
		return violations
	}

	// Retirement bars a Subject from a FIRST publication only. A Course that has
	// already published keeps its historical Subject as identity, so later
	// content revisions stay submittable and reviewable even after the Subject
	// is retired (D-093 §6).
	firstPublication := req.course.LiveRevisionID == nil || *req.course.LiveRevisionID == ""

	var owningInstitution string
	var retired bool
	err := req.tx.QueryRow(ctx, `
		SELECT institution_id::text, retired_at IS NOT NULL
		FROM subjects WHERE id = $1::uuid FOR SHARE
	`, *req.course.SubjectID).Scan(&owningInstitution, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return append(violations, SubmissionViolation{
			Code:      "ACADEMIC_SUBJECT_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: "SUBJECT",
		})
	}
	if err != nil {
		// A read failure here is reported as an unavailable dependency rather
		// than swallowed, so the submission fails closed.
		return append(violations, SubmissionViolation{
			Code:      "ACADEMIC_SUBJECT_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: "SUBJECT",
		})
	}
	if owningInstitution != *req.course.InstitutionID {
		return append(violations, SubmissionViolation{
			Code:      "ACADEMIC_SUBJECT_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: "SUBJECT",
		})
	}
	if retired && firstPublication {
		return append(violations, SubmissionViolation{
			Code:      "ACADEMIC_SUBJECT_RETIRED",
			Target:    "course:" + req.courseID,
			Dimension: "SUBJECT",
		})
	}
	// Direct academic-identity callers validate only the Course-level Subject;
	// submission and approval always supply a revision and additionally validate
	// its audience override.
	if req.revision == nil {
		return violations
	}
	invalidAudience, err := invalidExplicitAudience(
		ctx, req.tx, req.revision.ID, *req.course.InstitutionID, *req.course.SubjectID,
	)
	if err != nil || invalidAudience {
		return append(violations, SubmissionViolation{
			Code:      "ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE",
			Target:    "course:" + req.courseID,
			Dimension: "PROGRAM_AUDIENCE",
		})
	}
	return violations
}

func validateLessonFileDependency(ctx context.Context, req submissionValidationRequest, lesson Lesson, file LessonFile) error {
	if req.courseID == "" || req.revision.ID == "" || lesson.LessonIdentityID == "" || file.ID == "" || file.AssetVersionID == "" {
		return ErrAssetVersionInvalid
	}
	var found string
	err := req.tx.QueryRow(ctx, `
		SELECT mav.id::text
		FROM course_revisions cr
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = cr.course_id
		JOIN course_lessons cl ON cl.section_id = cs.id
			AND cl.course_id = cr.course_id AND cl.lesson_identity_id = $3::uuid
		JOIN lesson_files lf ON lf.lesson_id = cl.id
			AND lf.id = $4::uuid AND lf.asset_version_id = $5::uuid
		JOIN media_asset_versions mav ON mav.id = lf.asset_version_id
			AND mav.kind::text = lf.kind::text AND mav.state = 'READY'
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
			AND ma.course_id = cr.course_id AND ma.retired_at IS NULL
	`+media.ExactVersionProvenanceJoin+`
		WHERE cr.id = $2::uuid AND cr.course_id = $1::uuid
			AND lf.kind IN ('RESOURCE', 'LAB_MATERIAL')
		FOR SHARE OF cs, cl, lf, mav, ma
	`, req.courseID, req.revision.ID, lesson.LessonIdentityID, file.ID, file.AssetVersionID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetVersionInvalid
	}
	if err != nil {
		return fmt.Errorf("validating protected lesson file dependency: %w", err)
	}
	return nil
}

// validatePreviewAsset is the separate-public-media boundary. The supplied
// version must be a READY, scanner-cleared PREVIEW asset for this Course and
// must have been uploaded for this revision or one of its ancestors. The
// latter permits a candidate to inherit live Preview A unchanged, while the
// semantic set command requires a replacement Preview B to originate in B.
//
// The query locks the mutable Asset and Asset Version rows in the same
// transaction as submission/approval. A later retirement or readiness change
// therefore cannot race an approval into publishing an ineligible preview.
func validatePreviewAsset(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	revisionID string,
	assetVersionID string,
	requireCurrentOrigin bool,
) error {
	if tx == nil || courseID == "" || revisionID == "" || assetVersionID == "" {
		return ErrAssetVersionInvalid
	}
	originPredicate := `ma.preview_origin_revision_id = $3::uuid`
	if !requireCurrentOrigin {
		originPredicate = `EXISTS (SELECT 1 FROM lineage WHERE lineage.id = ma.preview_origin_revision_id)`
	}
	var found string
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE lineage AS (
			SELECT id, based_on_revision_id
			FROM course_revisions
			WHERE id = $3::uuid AND course_id = $2::uuid
			UNION ALL
			SELECT parent.id, parent.based_on_revision_id
			FROM course_revisions parent
			JOIN lineage child ON child.based_on_revision_id = parent.id
		)
		SELECT mav.id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN scan_attempts scan ON scan.id = mav.successful_scan_attempt_id
			AND scan.asset_version_id = mav.id
			AND scan.storage_object_version = mav.storage_object_version
			AND scan.outcome = 'PASSED'
		WHERE mav.id = $1::uuid
			AND ma.course_id = $2::uuid
			AND mav.kind = 'PREVIEW'
			AND ma.kind = 'PREVIEW'
			AND ma.visibility = 'PUBLIC_PREVIEW'
			AND ma.retired_at IS NULL
			AND mav.state = 'READY'
			AND mav.content_type = 'video/mp4'
			AND `+originPredicate+`
		FOR SHARE OF mav, ma
	`, assetVersionID, courseID, revisionID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetVersionInvalid
	}
	if err != nil {
		return fmt.Errorf("validating public preview asset: %w", err)
	}
	return nil
}

func taxonomyUnavailable(
	ctx context.Context,
	tx pgx.Tx,
	termID string,
	expectedKind TaxonomyKind,
) (bool, error) {
	var kind TaxonomyKind
	var retired bool
	err := tx.QueryRow(ctx, `
		SELECT kind, retired_at IS NOT NULL
		FROM taxonomy_terms
		WHERE id = $1::uuid
	`, termID).Scan(&kind, &retired)
	if err == pgx.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("validating taxonomy term %s: %w", termID, err)
	}
	return retired || kind != expectedKind, nil
}
