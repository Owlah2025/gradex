package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The Student academic profile (D-092).
//
// This is discovery-only personalisation data. Nothing in this file is read by
// entitlement, Course access, purchase, invitation, enrollment, progress, or
// playback. A Student may have no profile at all and still reach every Course
// they hold an entitlement for; changing a profile never changes access.

// SetupState is the Student's onboarding decision, recorded explicitly rather
// than inferred from which fields happen to be null.
type SetupState string

const (
	// SetupNotStarted is the absence of a row. It is never stored.
	SetupNotStarted SetupState = "NOT_STARTED"
	// SetupSkipped means the Student deliberately deferred. Gradex must not
	// prompt them again on every visit.
	SetupSkipped  SetupState = "SKIPPED"
	SetupComplete SetupState = "COMPLETED"
)

// EnrollmentStatus is the Student's own academic standing. An undeclared or
// non-degree Student is a state of the Student, never a placeholder Program.
type EnrollmentStatus string

const (
	EnrollmentEnrolled   EnrollmentStatus = "ENROLLED"
	EnrollmentUndeclared EnrollmentStatus = "UNDECLARED"
	EnrollmentFoundation EnrollmentStatus = "FOUNDATION"
	EnrollmentNonDegree  EnrollmentStatus = "NON_DEGREE"
)

func (s EnrollmentStatus) Valid() bool {
	switch s {
	case EnrollmentEnrolled, EnrollmentUndeclared, EnrollmentFoundation, EnrollmentNonDegree:
		return true
	}
	return false
}

var (
	ErrProfileNotFound         = errors.New("student academic profile not found")
	ErrProfileInvalid          = errors.New("student academic profile input is invalid")
	ErrCurriculumNotSelectable = errors.New("the curriculum is resolved by the server and cannot be supplied")
	ErrNoActiveCurriculum      = errors.New("the selected program has no active curriculum")
	ErrFoundationUnsupported   = errors.New("this institution declares no foundation stage")
	ErrUnitNotSelectable       = errors.New("the selected academic unit is not selectable for this institution")
	ErrProgramNotSelectable    = errors.New("the selected program is not selectable for this institution")
)

// StudentAcademicProfile is what the Student sees and edits. Names are resolved
// alongside identifiers so no surface has to look one up to render a label.
type StudentAcademicProfile struct {
	AccountID        string            `json:"-"`
	SetupState       SetupState        `json:"setup_state"`
	EnrollmentStatus *EnrollmentStatus `json:"enrollment_status,omitempty"`

	InstitutionID   *string `json:"institution_id,omitempty"`
	InstitutionName *string `json:"institution_name,omitempty"`
	// MaxAcademicLevel lets a surface render the level choices without a second
	// call and without hardcoding any institution's bounds.
	MaxAcademicLevel   *int  `json:"max_academic_level,omitempty"`
	HasFoundationStage *bool `json:"has_foundation_stage,omitempty"`

	// AcademicUnit is present only for Program-less states (D-092 §2). For an
	// enrolled Student the College is derived and reported in CollegeName.
	AcademicUnitID   *string `json:"academic_unit_id,omitempty"`
	AcademicUnitName *string `json:"academic_unit_name,omitempty"`

	ProgramID   *string `json:"program_id,omitempty"`
	ProgramName *string `json:"program_name,omitempty"`
	// DepartmentName and CollegeName are derived context for display. The
	// Student is never asked to choose a Department.
	DepartmentName *string `json:"department_name,omitempty"`
	CollegeName    *string `json:"college_name,omitempty"`

	CurriculumID    *string `json:"curriculum_id,omitempty"`
	CurriculumLabel *string `json:"curriculum_version_label,omitempty"`

	CurrentLevel *int `json:"current_level,omitempty"`

	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// SaveProfileRequest carries the Student's own choices. It deliberately has no
// curriculum field: a client that supplies one is refused (D-092 §6).
type SaveProfileRequest struct {
	AccountID        string
	InstitutionID    string
	EnrollmentStatus EnrollmentStatus
	// ProgramID is required for ENROLLED and must be absent otherwise.
	ProgramID string
	// AcademicUnitID carries the College for a Program-less Student.
	AcademicUnitID string
	CurrentLevel   *int
	// SuppliedCurriculumID is only ever set by a caller trying to choose one.
	// It exists so the refusal is explicit rather than a silently ignored field.
	SuppliedCurriculumID string
}

// GetProfile returns the Student's own profile. A Student with no row is
// NOT_STARTED, which is a normal state and never an error.
func (r *Repository) GetProfile(ctx context.Context, accountID string) (*StudentAcademicProfile, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ErrProfileNotFound
	}
	profile, err := r.loadProfile(ctx, r.pool, accountID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return &StudentAcademicProfile{AccountID: accountID, SetupState: SetupNotStarted}, nil
	}
	return profile, nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// loadProfile resolves display names in the same statement, including the
// College derived from an enrolled Student's Program ancestry. Nothing here
// stores that College: it is computed on read (D-092 §3).
func (r *Repository) loadProfile(ctx context.Context, q rowQuerier, accountID string) (*StudentAcademicProfile, error) {
	var p StudentAcademicProfile
	var status *string
	var setup string
	err := q.QueryRow(ctx, `
		SELECT sp.setup_state::text, sp.enrollment_status::text,
			sp.institution_id::text, i.name_en, i.max_academic_level, i.has_foundation_stage,
			sp.academic_unit_id::text, unit.name_en,
			sp.program_id::text, prog.name_en,
			department.name_en, college.name_en,
			sp.curriculum_id::text, cur.version_label,
			sp.current_level, sp.updated_at
		FROM student_academic_profiles sp
		LEFT JOIN institutions i ON i.id = sp.institution_id
		LEFT JOIN academic_units unit ON unit.id = sp.academic_unit_id
		LEFT JOIN programs prog ON prog.id = sp.program_id
		LEFT JOIN academic_units department ON department.id = prog.owning_unit_id
		LEFT JOIN academic_units college ON college.id = department.parent_unit_id
		LEFT JOIN curricula cur ON cur.id = sp.curriculum_id
		WHERE sp.account_id = $1::uuid
	`, accountID).Scan(&setup, &status,
		&p.InstitutionID, &p.InstitutionName, &p.MaxAcademicLevel, &p.HasFoundationStage,
		&p.AcademicUnitID, &p.AcademicUnitName,
		&p.ProgramID, &p.ProgramName,
		&p.DepartmentName, &p.CollegeName,
		&p.CurriculumID, &p.CurriculumLabel,
		&p.CurrentLevel, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, classifyConstraint(err)
	}
	p.AccountID = accountID
	p.SetupState = SetupState(setup)
	if status != nil {
		parsed := EnrollmentStatus(*status)
		p.EnrollmentStatus = &parsed
	}
	// A Program owned directly by a College rather than by a Department leaves
	// the derived College null while the Department join holds the College.
	// Report the College either way rather than showing a Department as one.
	if p.CollegeName == nil && p.DepartmentName != nil && p.ProgramID != nil {
		p.CollegeName = p.DepartmentName
		p.DepartmentName = nil
	}
	return &p, nil
}

// SkipOnboarding records an explicit deferral. It is a distinct domain action,
// never an empty save, and it clears any academic fields so a skipped profile
// can never be mistaken for a real one.
func (r *Repository) SkipOnboarding(ctx context.Context, accountID string) (*StudentAcademicProfile, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ErrProfileInvalid
	}
	var profile *StudentAcademicProfile
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO student_academic_profiles (account_id, setup_state)
			VALUES ($1::uuid, 'SKIPPED')
			ON CONFLICT (account_id) DO UPDATE
			SET setup_state = 'SKIPPED',
				enrollment_status = NULL, institution_id = NULL, academic_unit_id = NULL,
				program_id = NULL, curriculum_id = NULL, current_level = NULL,
				updated_at = now()
		`, accountID); err != nil {
			return classifyConstraint(err)
		}
		loaded, err := r.loadProfile(ctx, tx, accountID)
		if err != nil {
			return err
		}
		profile = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// SaveProfile validates the whole tuple server-side and resolves the Curriculum.
// A client can send any combination; only a coherent one is stored.
func (r *Repository) SaveProfile(ctx context.Context, req SaveProfileRequest) (*StudentAcademicProfile, error) {
	if strings.TrimSpace(req.AccountID) == "" {
		return nil, ErrProfileInvalid
	}
	// D-092 §6. Refusing explicitly beats ignoring the field, because a caller
	// that thinks it chose a plan must find out that it did not.
	if strings.TrimSpace(req.SuppliedCurriculumID) != "" {
		return nil, ErrCurriculumNotSelectable
	}
	if !req.EnrollmentStatus.Valid() {
		return nil, ErrProfileInvalid
	}
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrProfileInvalid
	}
	program := strings.TrimSpace(req.ProgramID)
	unit := strings.TrimSpace(req.AcademicUnitID)

	if req.EnrollmentStatus == EnrollmentEnrolled {
		if program == "" {
			return nil, ErrProfileInvalid
		}
		// The College an enrolled Student belongs to is derived from the
		// Program, so accepting one here would create a second source of truth.
		if unit != "" {
			return nil, ErrProfileInvalid
		}
	} else if program != "" {
		return nil, ErrProfileInvalid
	}

	var profile *StudentAcademicProfile
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		// Serialise concurrent saves for this account so two requests cannot
		// interleave into a row holding one field from each.
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext($1))`, "student-profile:"+req.AccountID); err != nil {
			return fmt.Errorf("acquiring profile lock: %w", err)
		}

		institution, err := lockInstitution(ctx, tx, req.InstitutionID)
		if err != nil {
			return err
		}
		if institution.RetiredAt != nil {
			return ErrRetired
		}
		if req.EnrollmentStatus == EnrollmentFoundation && !institution.HasFoundationStage {
			return ErrFoundationUnsupported
		}
		if req.CurrentLevel != nil {
			if *req.CurrentLevel < 1 || *req.CurrentLevel > institution.MaxAcademicLevel {
				return ErrLevelOutOfRange
			}
		}

		var unitArg, programArg, curriculumArg *string
		if unit != "" {
			if err := assertUnitInInstitution(ctx, tx, unit, institution.ID); err != nil {
				if errors.Is(err, ErrCrossInstitution) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrRetired) {
					return ErrUnitNotSelectable
				}
				return err
			}
			unitArg = &unit
		}

		if program != "" {
			resolved, err := r.resolveEnrolment(ctx, tx, institution.ID, program, req.AccountID)
			if err != nil {
				return err
			}
			programArg, curriculumArg = &program, resolved
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO student_academic_profiles (
				account_id, setup_state, enrollment_status, institution_id,
				academic_unit_id, program_id, curriculum_id, current_level
			) VALUES ($1::uuid, 'COMPLETED', $2, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7)
			ON CONFLICT (account_id) DO UPDATE
			SET setup_state = 'COMPLETED',
				enrollment_status = EXCLUDED.enrollment_status,
				institution_id = EXCLUDED.institution_id,
				academic_unit_id = EXCLUDED.academic_unit_id,
				program_id = EXCLUDED.program_id,
				curriculum_id = EXCLUDED.curriculum_id,
				current_level = EXCLUDED.current_level,
				updated_at = now()
		`, req.AccountID, string(req.EnrollmentStatus), institution.ID,
			unitArg, programArg, curriculumArg, req.CurrentLevel); err != nil {
			return classifyConstraint(err)
		}

		loaded, err := r.loadProfile(ctx, tx, req.AccountID)
		if err != nil {
			return err
		}
		profile = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// resolveEnrolment picks the Curriculum for a Program the Student selected.
//
// D-092 §6: a Student stays on the plan they enrolled under. If the profile
// already names this same Program, the existing curriculum is preserved even
// when a newer one has since become ACTIVE — editing a level must never migrate
// a Student's plan. Only a Program change resolves the current ACTIVE plan.
func (r *Repository) resolveEnrolment(
	ctx context.Context, tx pgx.Tx, institutionID, programID, accountID string,
) (*string, error) {
	var owner string
	var retiredAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT institution_id::text, retired_at FROM programs WHERE id = $1::uuid
	`, programID).Scan(&owner, &retiredAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProgramNotSelectable
		}
		classified := classifyConstraint(err)
		if errors.Is(classified, ErrNotFound) {
			return nil, ErrProgramNotSelectable
		}
		return nil, classified
	}
	// A retired Program stays readable on an existing profile but can never be
	// newly selected.
	if owner != institutionID || retiredAt != nil {
		return nil, ErrProgramNotSelectable
	}

	var existingProgram, existingCurriculum *string
	if err := tx.QueryRow(ctx, `
		SELECT program_id::text, curriculum_id::text FROM student_academic_profiles
		WHERE account_id = $1::uuid
	`, accountID).Scan(&existingProgram, &existingCurriculum); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, classifyConstraint(err)
	}
	if existingProgram != nil && *existingProgram == programID && existingCurriculum != nil {
		return existingCurriculum, nil
	}

	var curriculum string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM curricula
		WHERE program_id = $1::uuid AND status = 'ACTIVE' AND retired_at IS NULL
	`, programID).Scan(&curriculum); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A clean, nameable refusal rather than a constraint violation.
			return nil, ErrNoActiveCurriculum
		}
		return nil, classifyConstraint(err)
	}
	return &curriculum, nil
}
