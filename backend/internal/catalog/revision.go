package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RevisionState string

const (
	RevisionDraft            RevisionState = "DRAFT"
	RevisionPendingReview    RevisionState = "PENDING_REVIEW"
	RevisionChangesRequested RevisionState = "CHANGES_REQUESTED"
	RevisionApproved         RevisionState = "APPROVED"
	RevisionSuperseded       RevisionState = "SUPERSEDED"
	RevisionRejected         RevisionState = "REJECTED"
)

func (s RevisionState) Valid() bool {
	switch s {
	case RevisionDraft, RevisionPendingReview, RevisionChangesRequested, RevisionApproved, RevisionSuperseded, RevisionRejected:
		return true
	default:
		return false
	}
}

type StudyYear string

const (
	StudyYearPrep  StudyYear = "PREP"
	StudyYearYear1 StudyYear = "YEAR_1"
	StudyYearYear2 StudyYear = "YEAR_2"
	StudyYearYear3 StudyYear = "YEAR_3"
	StudyYearYear4 StudyYear = "YEAR_4"
)

func (y StudyYear) Valid() bool {
	switch y {
	case StudyYearPrep, StudyYearYear1, StudyYearYear2, StudyYearYear3, StudyYearYear4:
		return true
	default:
		return false
	}
}

type TaxonomyKind string

const (
	TaxonomyMajor   TaxonomyKind = "MAJOR"
	TaxonomySubject TaxonomyKind = "SUBJECT"
)

func (k TaxonomyKind) Valid() bool {
	return k == TaxonomyMajor || k == TaxonomySubject
}

type LessonFileKind string

const (
	FileKindResource    LessonFileKind = "RESOURCE"
	FileKindLabMaterial LessonFileKind = "LAB_MATERIAL"
)

func (k LessonFileKind) Valid() bool {
	return k == FileKindResource || k == FileKindLabMaterial
}

var (
	ErrAssetVersionInvalid  = errors.New("asset version reference is invalid or not found")
	ErrAssetVersionNotReady = errors.New("asset version is not in ready state")
)

// AssetVersionValidator validates Asset Version references (T014, SLICES §3.2).
// Absent or unprocessed versions are refused.
// No upload, scan, or transcode path is included.
type AssetVersionValidator interface {
	ValidateAssetVersion(ctx context.Context, assetVersionID string) error
}

type TaxonomyTerm struct {
	ID           string       `json:"id"`
	Kind         TaxonomyKind `json:"kind"`
	LabelAr      string       `json:"label_ar"`
	LabelEn      string       `json:"label_en"`
	AcademicCode *string      `json:"academic_code,omitempty"`
	RetiredAt    *time.Time   `json:"retired_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type CourseRevision struct {
	ID                    string        `json:"id"`
	CourseID              string        `json:"course_id"`
	BasedOnRevisionID     *string       `json:"based_on_revision_id,omitempty"`
	State                 RevisionState `json:"state"`
	RevisionNumber        int           `json:"revision_number"`
	TitleAr               string        `json:"title_ar"`
	TitleEn               string        `json:"title_en"`
	DescriptionAr         string        `json:"description_ar"`
	DescriptionEn         string        `json:"description_en"`
	MajorTermID           *string       `json:"major_term_id,omitempty"`
	SubjectTermID         *string       `json:"subject_term_id,omitempty"`
	StudyYear             *StudyYear    `json:"study_year,omitempty"`
	PreviewAssetVersionID *string       `json:"preview_asset_version_id,omitempty"`
	SubmittedAt           *time.Time    `json:"submitted_at,omitempty"`
	ReviewedAt            *time.Time    `json:"reviewed_at,omitempty"`
	ReviewedByAccountID   *string       `json:"reviewed_by_account_id,omitempty"`
	ReviewReason          *string       `json:"review_reason,omitempty"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
	Sections              []Section     `json:"sections"`
}

func (r *CourseRevision) ValidateInvariants() error {
	if r.CourseID == "" {
		return errors.New("revision course_id is required")
	}
	if len(r.TitleAr) == 0 || len(r.TitleEn) == 0 {
		return errors.New("revision title_ar and title_en are required")
	}
	if r.StudyYear != nil && !r.StudyYear.Valid() {
		return fmt.Errorf("invalid study_year: %s", *r.StudyYear)
	}
	return nil
}

type Section struct {
	ID                string    `json:"-"`
	RevisionID        string    `json:"revision_id"`
	CourseID          string    `json:"course_id"`
	SectionIdentityID string    `json:"id"`
	TitleAr           string    `json:"title_ar"`
	TitleEn           string    `json:"title_en"`
	Position          int       `json:"position"`
	PriceMinorUnits   *int64    `json:"price_minor_units,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Lessons           []Lesson  `json:"lessons"`
}

type Lesson struct {
	ID                  string       `json:"-"`
	SectionID           string       `json:"-"`
	CourseID            string       `json:"course_id"`
	SectionIdentityID   string       `json:"section_id"`
	LessonIdentityID    string       `json:"id"`
	TitleAr             string       `json:"title_ar"`
	TitleEn             string       `json:"title_en"`
	Position            int          `json:"position"`
	VideoAssetVersionID *string      `json:"video_asset_version_id,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
	Files               []LessonFile `json:"files"`
}

type LessonFile struct {
	ID             string         `json:"id"`
	LessonID       string         `json:"-"`
	Kind           LessonFileKind `json:"kind"`
	AssetVersionID string         `json:"asset_version_id"`
	DisplayNameAr  string         `json:"display_name_ar"`
	DisplayNameEn  string         `json:"display_name_en"`
	Position       int            `json:"position"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
