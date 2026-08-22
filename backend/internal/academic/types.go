// Package academic owns the canonical, Institution-scoped Academic Catalog
// established by D-091.
//
// Scope boundary (T1 / MVP-F17): this package is additive and inert with
// respect to Courses. It is not read or written by any Course authoring,
// review, catalogue, entitlement, access, purchase, or media path. The legacy
// taxonomy_terms vocabulary remains authoritative for Course classification
// until the T5 cutover proves the dual path.
//
// The package deliberately holds no degree-audit, prerequisite, credit
// accumulation, graduation, GPA, registration, scheduling, or transcript logic.
// requirement_kind, recommended_level, recommended_semester, and credits are
// metadata; nothing computes over them.
package academic

import (
	"errors"
	"time"
)

// UnitKind is the flexible academic-hierarchy discriminator. A COLLEGE and a
// DEPARTMENT are the same kind of node with different labels, because the
// observed institutional shapes disagree about whether a department layer
// exists at all.
type UnitKind string

const (
	UnitKindCollege     UnitKind = "COLLEGE"
	UnitKindDepartment  UnitKind = "DEPARTMENT"
	UnitKindServiceUnit UnitKind = "SERVICE_UNIT"
)

func (k UnitKind) Valid() bool {
	switch k {
	case UnitKindCollege, UnitKindDepartment, UnitKindServiceUnit:
		return true
	}
	return false
}

// CurriculumStatus tracks which academic plan version is in force. Exactly one
// ACTIVE per Program is a database invariant, not an application convention.
type CurriculumStatus string

const (
	CurriculumActive     CurriculumStatus = "ACTIVE"
	CurriculumSuperseded CurriculumStatus = "SUPERSEDED"
)

func (s CurriculumStatus) Valid() bool {
	return s == CurriculumActive || s == CurriculumSuperseded
}

// RequirementKind names the academic-plan category a Subject occupies inside a
// Curriculum. The values mirror the requirement groupings Kuwait University,
// AUK, and AUM actually publish.
type RequirementKind string

const (
	RequirementUniversity RequirementKind = "UNIVERSITY_REQUIREMENT"
	RequirementCollege    RequirementKind = "COLLEGE_REQUIREMENT"
	RequirementMajorCore  RequirementKind = "MAJOR_CORE"
	RequirementMajorElec  RequirementKind = "MAJOR_ELECTIVE"
	RequirementSupporting RequirementKind = "SUPPORTING"
	RequirementFree       RequirementKind = "FREE_ELECTIVE"
)

func (k RequirementKind) Valid() bool {
	switch k {
	case RequirementUniversity, RequirementCollege, RequirementMajorCore,
		RequirementMajorElec, RequirementSupporting, RequirementFree:
		return true
	}
	return false
}

var (
	ErrRepositoryNil = errors.New("academic repository requires a database pool")

	ErrNotFound          = errors.New("academic catalog entity not found")
	ErrInvalidInput      = errors.New("academic catalog input is invalid")
	ErrAdminRequired     = errors.New("academic catalog mutation requires an Admin principal")
	ErrRetired           = errors.New("academic catalog entity is retired")
	ErrCrossInstitution  = errors.New("academic catalog relationship crosses institutions")
	ErrHierarchyCycle    = errors.New("academic unit hierarchy would contain a cycle")
	ErrSlugTaken         = errors.New("academic catalog slug is already used in this institution")
	ErrDuplicateSubject  = errors.New("an equivalent subject already exists in this institution")
	ErrCurriculumActive  = errors.New("this program already has an active curriculum")
	ErrLevelOutOfRange   = errors.New("recommended level exceeds the institution maximum")
	ErrMappingDuplicate  = errors.New("this subject is already mapped into this curriculum")
	ErrStillReferenced   = errors.New("academic catalog entity is still referenced")
	ErrVersionLabelTaken = errors.New("this program already has a curriculum with that version label")

	// ErrSubjectCodeImmutable enforces the amended D-093 7: a normalized official
	// code is part of canonical Subject identity, so once established it cannot
	// be changed to a different normalized identity, and cannot be withdrawn.
	//
	// The unique index reserves a code against other Subjects. It cannot stop the
	// holder from releasing it — by renumbering itself, or by clearing the code —
	// which would free the identity for a different Subject while a published
	// Course still points at the original. That is what this refuses.
	//
	// Display formatting that preserves the normalized form is NOT affected:
	// '0418 320' and '0418-320' are the same identity.
	ErrSubjectCodeImmutable = errors.New("a subject's official code is canonical identity and cannot be changed once established")
)

// DuplicateSubjectError carries the existing Subject so the Admin surface can
// name it instead of silently failing. A conflict must always be actionable.
type DuplicateSubjectError struct {
	Existing *Subject
}

func (e *DuplicateSubjectError) Error() string { return ErrDuplicateSubject.Error() }
func (e *DuplicateSubjectError) Unwrap() error { return ErrDuplicateSubject }

type Institution struct {
	ID                 string     `json:"id"`
	CountryCode        string     `json:"country_code"`
	Slug               string     `json:"slug"`
	NameAr             string     `json:"name_ar"`
	NameEn             string     `json:"name_en"`
	MaxAcademicLevel   int        `json:"max_academic_level"`
	HasFoundationStage bool       `json:"has_foundation_stage"`
	RetiredAt          *time.Time `json:"retired_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type AcademicUnit struct {
	ID            string     `json:"id"`
	InstitutionID string     `json:"institution_id"`
	ParentUnitID  *string    `json:"parent_unit_id,omitempty"`
	Kind          UnitKind   `json:"kind"`
	Slug          string     `json:"slug"`
	NameAr        string     `json:"name_ar"`
	NameEn        string     `json:"name_en"`
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Program struct {
	ID            string     `json:"id"`
	InstitutionID string     `json:"institution_id"`
	OwningUnitID  *string    `json:"owning_unit_id,omitempty"`
	Slug          string     `json:"slug"`
	NameAr        string     `json:"name_ar"`
	NameEn        string     `json:"name_en"`
	DegreeKind    string     `json:"degree_kind"`
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Curriculum struct {
	ID                string           `json:"id"`
	ProgramID         string           `json:"program_id"`
	InstitutionID     string           `json:"institution_id"`
	VersionLabel      string           `json:"version_label"`
	EffectiveFromYear *int             `json:"effective_from_year,omitempty"`
	Status            CurriculumStatus `json:"status"`
	RetiredAt         *time.Time       `json:"retired_at,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type Subject struct {
	ID            string     `json:"id"`
	InstitutionID string     `json:"institution_id"`
	OwningUnitID  *string    `json:"owning_unit_id,omitempty"`
	OfficialCode  *string    `json:"official_code,omitempty"`
	TitleAr       string     `json:"title_ar"`
	TitleEn       string     `json:"title_en"`
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// CurriculumSubject is the many-to-many edge that lets one canonical Subject
// serve many Programs without being duplicated.
type CurriculumSubject struct {
	ID                  string          `json:"id"`
	CurriculumID        string          `json:"curriculum_id"`
	SubjectID           string          `json:"subject_id"`
	InstitutionID       string          `json:"institution_id"`
	RequirementKind     RequirementKind `json:"requirement_kind"`
	RecommendedLevel    *int            `json:"recommended_level,omitempty"`
	RecommendedSemester *int            `json:"recommended_semester,omitempty"`
	Credits             *float64        `json:"credits,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`

	// Denormalised for the Admin mapping table so the surface never has to
	// resolve an identifier client-side.
	SubjectOfficialCode *string `json:"subject_official_code,omitempty"`
	SubjectTitleAr      string  `json:"subject_title_ar,omitempty"`
	SubjectTitleEn      string  `json:"subject_title_en,omitempty"`
}
