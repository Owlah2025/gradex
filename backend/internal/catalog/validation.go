package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
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

	if req.revision == nil {
		violations = append(violations, SubmissionViolation{
			Code:   "COURSE_EMPTY",
			Target: "course:" + req.courseID,
		})
		return &SubmissionValidationError{Violations: violations}, nil
	}

	// 1. Taxonomy dimension validation (FR-010)
	if req.revision.MajorTermID == nil || *req.revision.MajorTermID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + req.courseID,
			Dimension: string(TaxonomyMajor),
		})
	} else if unavailable, err := taxonomyUnavailable(ctx, req.tx, *req.revision.MajorTermID, TaxonomyMajor); err != nil {
		return nil, err
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
		return nil, err
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

	if req.revision.PreviewAssetVersionID != nil && *req.revision.PreviewAssetVersionID != "" {
		if err := req.validator.ValidateAssetVersion(ctx, *req.revision.PreviewAssetVersionID); err != nil {
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
						if err := req.validator.ValidateAssetVersion(ctx, file.AssetVersionID); err != nil {
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
