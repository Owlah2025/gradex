package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAccountProfileNotFound = errors.New("student account profile not found")
	ErrAccountProfileInvalid  = errors.New("student account profile update is invalid")
)

type AcademicLabels struct {
	Institution string `json:"institution,omitempty"`
	CollegeUnit string `json:"college_unit,omitempty"`
	Program     string `json:"program,omitempty"`
	Level       *int   `json:"level,omitempty"`
}

type AccountProfile struct {
	DisplayName     string          `json:"display_name"`
	Email           string          `json:"email"`
	Locale          Locale          `json:"locale"`
	Status          string          `json:"status"`
	AcademicProfile *AcademicLabels `json:"academic_profile,omitempty"`
	// WhatsAppNumber is the Student's own number in E.164, or null when none
	// was provided. It is "provided", never "verified": Gradex does not
	// confirm that the number belongs to the Student.
	WhatsAppNumber          *string    `json:"whatsapp_number"`
	WhatsAppNumberUpdatedAt *time.Time `json:"whatsapp_number_updated_at,omitempty"`
}

type AccountProfileUpdateRequest struct {
	AccountID   string
	DisplayName *string
	Locale      *Locale
	// WhatsAppNumber replaces the Student's number. It cannot clear one: a
	// provided number is what keeps a pending purchase reachable.
	WhatsAppNumber *string
}

type AccountProfileService struct {
	pool *pgxpool.Pool
}

func NewAccountProfileService(pool *pgxpool.Pool) (*AccountProfileService, error) {
	if pool == nil {
		return nil, errors.New("account profile database pool is required")
	}
	return &AccountProfileService{pool: pool}, nil
}

func (s *AccountProfileService) Get(ctx context.Context, accountID string) (*AccountProfile, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, ErrAccountProfileNotFound
	}
	profile, err := readAccountProfile(ctx, s.pool, accountID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *AccountProfileService) Update(
	ctx context.Context,
	request AccountProfileUpdateRequest,
) (*AccountProfile, error) {
	if err := validateAccountProfileUpdate(request); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning account profile update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var role, currentDisplayName, currentLocale string
	var currentWhatsApp *string
	if err := tx.QueryRow(ctx, `
		SELECT role::text, display_name, locale::text, contact_phone
		FROM accounts
		WHERE id = $1::uuid AND role = 'STUDENT'
		FOR UPDATE
	`, request.AccountID).Scan(&role, &currentDisplayName, &currentLocale, &currentWhatsApp); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountProfileNotFound
		}
		return nil, fmt.Errorf("locking student account profile: %w", err)
	}

	changedFields := make([]string, 0, 3)
	var displayName any
	if request.DisplayName != nil {
		normalized, err := ValidateDisplayName(*request.DisplayName)
		if err != nil {
			return nil, err
		}
		displayName = normalized
		if normalized != currentDisplayName {
			changedFields = append(changedFields, "display_name")
		}
	}
	var locale any
	if request.Locale != nil {
		normalized := string(*request.Locale)
		locale = normalized
		if normalized != currentLocale {
			changedFields = append(changedFields, "locale")
		}
	}

	var whatsAppNumber any
	if request.WhatsAppNumber != nil {
		normalized, err := NormalizeWhatsAppNumber(*request.WhatsAppNumber)
		if err != nil {
			return nil, err
		}
		if currentWhatsApp == nil || *currentWhatsApp != normalized {
			whatsAppNumber = normalized
			changedFields = append(changedFields, "whatsapp_number")
		}
	}

	var revision int
	if err := tx.QueryRow(ctx, `
		UPDATE accounts
		SET display_name = COALESCE($2, display_name),
			locale = COALESCE($3, locale),
			contact_phone = COALESCE($4, contact_phone),
			contact_phone_updated_at = CASE WHEN $4::text IS NULL THEN contact_phone_updated_at ELSE now() END,
			revision = revision + 1,
			updated_at = now()
		WHERE id = $1::uuid AND role = 'STUDENT'
		RETURNING revision
	`, request.AccountID, displayName, locale, whatsAppNumber).Scan(&revision); err != nil {
		return nil, fmt.Errorf("updating student account profile: %w", err)
	}

	// Field names only. The number itself never enters the audit trail, which
	// is readable far more widely than the profile it describes.
	metadata, err := json.Marshal(map[string]any{"changed_fields": changedFields})
	if err != nil {
		return nil, fmt.Errorf("encoding account profile audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module,
			target_type, target_id, target_revision, reason, metadata
		) VALUES (
			$1::uuid, $2, $1::text, 'ACCOUNT_PROFILE_UPDATED', 'IDENTITY_AND_ACCESS',
			'ACCOUNT', $1::text, $3, 'Student updated their own profile', $4::jsonb
		)
	`, request.AccountID, role, revision, metadata); err != nil {
		return nil, fmt.Errorf("writing account profile audit event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing account profile update: %w", err)
	}

	return readAccountProfile(ctx, s.pool, request.AccountID)
}

func validateAccountProfileUpdate(request AccountProfileUpdateRequest) error {
	if strings.TrimSpace(request.AccountID) == "" {
		return ErrAccountProfileInvalid
	}
	if request.DisplayName == nil && request.Locale == nil && request.WhatsAppNumber == nil {
		return ErrAccountProfileInvalid
	}
	if request.DisplayName != nil {
		if _, err := ValidateDisplayName(*request.DisplayName); err != nil {
			return err
		}
	}
	if request.Locale != nil && !request.Locale.Valid() {
		return ErrInvalidLocale
	}
	if request.WhatsAppNumber != nil {
		if _, err := NormalizeWhatsAppNumber(*request.WhatsAppNumber); err != nil {
			return err
		}
	}
	return nil
}

func readAccountProfile(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, accountID string) (*AccountProfile, error) {
	profile := &AccountProfile{}
	var locale, status string
	var institution, collegeUnit, program *string
	var level *int
	err := q.QueryRow(ctx, `
		SELECT a.display_name, a.email, a.locale, a.status::text,
			CASE WHEN a.locale = 'ar' THEN institution.name_ar ELSE institution.name_en END,
			CASE
				WHEN student.program_id IS NULL THEN
					CASE WHEN a.locale = 'ar' THEN selected_unit.name_ar ELSE selected_unit.name_en END
				ELSE
					CASE WHEN a.locale = 'ar'
						THEN COALESCE(college.name_ar, owning_unit.name_ar)
						ELSE COALESCE(college.name_en, owning_unit.name_en)
					END
			END,
			CASE WHEN a.locale = 'ar' THEN program.name_ar ELSE program.name_en END,
			student.current_level,
			a.contact_phone, a.contact_phone_updated_at
		FROM accounts a
		LEFT JOIN student_academic_profiles student ON student.account_id = a.id
		LEFT JOIN institutions institution ON institution.id = student.institution_id
		LEFT JOIN academic_units selected_unit ON selected_unit.id = student.academic_unit_id
		LEFT JOIN programs program ON program.id = student.program_id
		LEFT JOIN academic_units owning_unit ON owning_unit.id = program.owning_unit_id
		LEFT JOIN academic_units college ON college.id = owning_unit.parent_unit_id
		WHERE a.id = $1::uuid AND a.role = 'STUDENT'
	`, accountID).Scan(
		&profile.DisplayName, &profile.Email, &locale, &status,
		&institution, &collegeUnit, &program, &level,
		&profile.WhatsAppNumber, &profile.WhatsAppNumberUpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountProfileNotFound
		}
		return nil, fmt.Errorf("reading student account profile: %w", err)
	}
	profile.Locale = Locale(locale)
	profile.Status = status
	if institution != nil || collegeUnit != nil || program != nil || level != nil {
		profile.AcademicProfile = &AcademicLabels{
			Institution: valueOrEmpty(institution),
			CollegeUnit: valueOrEmpty(collegeUnit),
			Program:     valueOrEmpty(program),
			Level:       level,
		}
	}
	return profile, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
