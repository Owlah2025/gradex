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

func (e *SubmissionValidationError) Error() string {
	return fmt.Sprintf("course submission incomplete: %d violations", len(e.Violations))
}

// ValidateCourseForSubmission validates a course revision graph for submission completeness (FR-009, FR-010).
// CRITICAL: It collects ALL violations into a single list rather than returning on the first failure.
func ValidateCourseForSubmission(
	ctx context.Context,
	tx pgx.Tx,
	validator AssetVersionValidator,
	courseID string,
	rev *CourseRevision,
) (*SubmissionValidationError, error) {
	var violations []SubmissionViolation

	if rev == nil {
		violations = append(violations, SubmissionViolation{
			Code:   "COURSE_EMPTY",
			Target: "course:" + courseID,
		})
		return &SubmissionValidationError{Violations: violations}, nil
	}

	// 1. Taxonomy dimension validation (FR-010)
	if rev.MajorTermID == nil || *rev.MajorTermID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + courseID,
			Dimension: "MAJOR",
		})
	}
	if rev.SubjectTermID == nil || *rev.SubjectTermID == "" {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + courseID,
			Dimension: "SUBJECT",
		})
	}
	if rev.StudyYear == nil {
		violations = append(violations, SubmissionViolation{
			Code:      "TAXONOMY_DIMENSION_MISSING",
			Target:    "course:" + courseID,
			Dimension: "STUDY_YEAR",
		})
	}

	// 2. Sections and Lessons completeness validation (FR-009)
	if len(rev.Sections) == 0 {
		violations = append(violations, SubmissionViolation{
			Code:   "COURSE_EMPTY",
			Target: "course:" + courseID,
		})
	} else {
		for _, sec := range rev.Sections {
			if len(sec.Lessons) == 0 {
				violations = append(violations, SubmissionViolation{
					Code:   "SECTION_EMPTY",
					Target: "section:" + sec.ID,
				})
			} else {
				for _, les := range sec.Lessons {
					if les.VideoAssetVersionID == nil || *les.VideoAssetVersionID == "" {
						violations = append(violations, SubmissionViolation{
							Code:   "LESSON_VIDEO_MISSING",
							Target: "lesson:" + les.ID,
						})
					} else if validator != nil {
						if err := validator.ValidateAssetVersion(ctx, *les.VideoAssetVersionID); err != nil {
							violations = append(violations, SubmissionViolation{
								Code:   "LESSON_VIDEO_MISSING",
								Target: "lesson:" + les.ID,
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
