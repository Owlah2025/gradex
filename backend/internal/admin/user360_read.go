package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	user360CourseLimit      = 100
	user360EntitlementLimit = 100
	user360HistoryLimit     = 50
	user360SecurityLimit    = 10
	user360AuditLimit       = 10
	user360NotesLimit       = 20
)

func (r *Repository) GetUser360(ctx context.Context, req User360Request) (User360, error) {
	if err := validateUser360Request(req); err != nil {
		return User360{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User360{}, fmt.Errorf("beginning User 360 read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	identityView, err := queryUser360Identity(ctx, tx, req.Locale, req.AccountID)
	if err != nil {
		return User360{}, err
	}
	identityView.LastLearningActivityAt, err = queryLastLearningActivity(ctx, tx, req.AccountID)
	if err != nil {
		return User360{}, err
	}

	result := User360{Identity: identityView}
	switch identityView.Role {
	case identity.RoleStudent:
		student, err := r.queryStudentUser360(ctx, tx, req.Locale, identityView)
		if err != nil {
			return User360{}, err
		}
		result.Student = &student
	case identity.RoleInstructor:
		instructor, err := queryInstructorUser360(ctx, tx, req.Locale, identityView)
		if err != nil {
			return User360{}, err
		}
		result.Instructor = &instructor
	}

	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     req.Principal,
		CorrelationID: req.CorrelationID,
		Action:        ActionUserViewed,
		Module:        catalog.AuditModuleIdentityAndAccess,
		TargetType:    User360TargetType,
		TargetID:      req.AccountID,
		Reason:        "privileged read: User 360 account view",
		Metadata: map[string]any{
			"role":     string(identityView.Role),
			"sections": user360Sections(identityView.Role),
		},
	}); err != nil {
		return User360{}, fmt.Errorf("auditing User 360 read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User360{}, fmt.Errorf("committing User 360 read: %w", err)
	}
	return result, nil
}

func validateUser360Request(req User360Request) error {
	if err := validateIdentityRead(req.Principal); err != nil {
		return err
	}
	if !req.Locale.Valid() {
		return ErrInvalidInput
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return ErrAccountNotFound
	}
	return nil
}

func user360Sections(role identity.Role) []string {
	switch role {
	case identity.RoleStudent:
		return []string{"identity", "academic_profile", "courses", "entitlements", "invitations", "purchase_requests", "devices", "security_events", "audit_events", "notes"}
	case identity.RoleInstructor:
		return []string{"identity", "owned_courses", "audit_events", "notes"}
	default:
		return []string{"identity"}
	}
}

func queryUser360Identity(ctx context.Context, tx pgx.Tx, locale identity.Locale, accountID string) (User360Identity, error) {
	var view User360Identity
	var role, status, accountLocale string
	err := tx.QueryRow(ctx, `
		SELECT a.id::text, a.display_name, a.email, a.role::text, a.status::text,
		       a.locale, a.email_verified_at IS NOT NULL, a.created_at,
		       MAX(s.last_activity_at)
		  FROM accounts a
		  LEFT JOIN sessions s ON s.account_id = a.id
		 WHERE a.id = $1::uuid
		 GROUP BY a.id`, accountID).Scan(
		&view.ID, &view.DisplayName, &view.Email, &role, &status, &accountLocale,
		&view.EmailVerified, &view.CreatedAt, &view.LastSignInActivityAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User360Identity{}, ErrAccountNotFound
	}
	if err != nil {
		return User360Identity{}, fmt.Errorf("querying User 360 identity: %w", err)
	}
	view.Role = identity.Role(role)
	view.Status = identity.AccountStatus(status)
	view.Locale = identity.Locale(accountLocale)
	if !view.Locale.Valid() {
		view.Locale = locale
	}
	return view, nil
}

func queryLastLearningActivity(ctx context.Context, tx pgx.Tx, accountID string) (*time.Time, error) {
	var value *time.Time
	err := tx.QueryRow(ctx, `
		SELECT MAX(p.last_watched_at)
		  FROM enrollments e
		  JOIN progress p ON p.enrollment_id = e.id
		 WHERE e.student_account_id = $1::uuid`, accountID).Scan(&value)
	if err != nil {
		return nil, fmt.Errorf("querying last learning activity: %w", err)
	}
	if value != nil {
		utc := value.UTC()
		return &utc, nil
	}
	return nil, nil
}

func (r *Repository) queryStudentUser360(
	ctx context.Context,
	tx pgx.Tx,
	locale identity.Locale,
	identityView User360Identity,
) (StudentUser360, error) {
	student := StudentUser360{
		Courses: []UserCourse{}, Entitlements: []UserEntitlement{}, Invitations: []UserInvitation{},
		PurchaseRequests: []UserPurchaseRequest{}, SecurityEvents: []SecurityEvent{}, AuditEvents: []AuditEvent{}, Notes: []AdminNote{},
	}
	var err error
	student.AcademicProfile, err = queryAcademicProfile(ctx, tx, locale, identityView.ID)
	if err != nil {
		return StudentUser360{}, err
	}
	student.Courses, err = r.queryStudentCourses(ctx, locale, identityView.ID)
	if err != nil {
		return StudentUser360{}, err
	}
	student.Entitlements, err = queryEntitlements(ctx, tx, locale, identityView.ID)
	if err != nil {
		return StudentUser360{}, err
	}
	student.Invitations, err = queryInvitations(ctx, tx, locale, identityView.Email)
	if err != nil {
		return StudentUser360{}, err
	}
	student.PurchaseRequests, err = queryPurchaseRequests(ctx, tx, locale, identityView.ID, identityView.Email)
	if err != nil {
		return StudentUser360{}, err
	}
	student.Devices, err = r.readDevices(ctx, identityView.ID)
	if err != nil {
		return StudentUser360{}, err
	}
	student.SecurityEvents, err = querySecurityEvents(ctx, tx, identityView.ID, 1, user360SecurityLimit)
	if err != nil {
		return StudentUser360{}, err
	}
	student.AuditEvents, err = queryTargetAuditEvents(ctx, tx, identityView.ID, user360AuditLimit)
	if err != nil {
		return StudentUser360{}, err
	}
	notes, err := queryNotes(ctx, tx, identityView.ID, user360NotesLimit)
	if err != nil {
		return StudentUser360{}, err
	}
	student.Notes = notes
	return student, nil
}

func queryAcademicProfile(ctx context.Context, tx pgx.Tx, locale identity.Locale, accountID string) (*AcademicProfile, error) {
	var profile AcademicProfile
	var enrollmentStatus, institutionAr, institutionEn, unitAr, unitEn, programAr, programEn, curriculumLabel *string
	err := tx.QueryRow(ctx, `
		SELECT sap.setup_state::text, sap.enrollment_status::text,
		       i.name_ar, i.name_en, au.name_ar, au.name_en,
		       p.name_ar, p.name_en, c.version_label, sap.current_level
		  FROM student_academic_profiles sap
		  LEFT JOIN institutions i ON i.id = sap.institution_id
		  LEFT JOIN academic_units au ON au.id = sap.academic_unit_id
		  LEFT JOIN programs p ON p.id = sap.program_id
		  LEFT JOIN curricula c ON c.id = sap.curriculum_id
		 WHERE sap.account_id = $1::uuid`, accountID).Scan(
		&profile.SetupState, &enrollmentStatus, &institutionAr, &institutionEn,
		&unitAr, &unitEn, &programAr, &programEn, &curriculumLabel, &profile.CurrentLevel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying academic profile: %w", err)
	}
	if enrollmentStatus != nil {
		profile.EnrollmentStatus = *enrollmentStatus
	}
	profile.InstitutionLabel = localizedInstitution(locale, institutionAr, institutionEn)
	profile.AcademicUnitLabel = localizedInstitution(locale, unitAr, unitEn)
	profile.ProgramLabel = localizedInstitution(locale, programAr, programEn)
	if curriculumLabel != nil {
		profile.CurriculumLabel = *curriculumLabel
	}
	return &profile, nil
}

func (r *Repository) queryStudentCourses(ctx context.Context, locale identity.Locale, accountID string) ([]UserCourse, error) {
	if r.learning == nil {
		return []UserCourse{}, nil
	}
	summaries, err := r.learning.ListStudentCourseSummaries(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("querying student course progress: %w", err)
	}
	if len(summaries) > user360CourseLimit {
		summaries = summaries[:user360CourseLimit]
	}
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.CourseID)
	}
	metadata, err := r.courseMetadata(ctx, locale, ids)
	if err != nil {
		return nil, err
	}
	result := make([]UserCourse, 0, len(summaries))
	for _, summary := range summaries {
		item := UserCourse{ID: summary.CourseID, Title: localizedTitle(locale, summary.TitleAr, summary.TitleEn),
			CompletedLessons: summary.Progress.CompletedLessons, TotalLessons: summary.Progress.TotalLessons,
			LastWatchedAt: summary.LastWatchedAt}
		if item.TotalLessons > 0 {
			item.ProgressPercent = float64(item.CompletedLessons) * 100 / float64(item.TotalLessons)
		}
		if details, ok := metadata[summary.CourseID]; ok {
			item.Lifecycle = details.lifecycle
			item.CandidateRevisionState = details.candidateState
		}
		result = append(result, item)
	}
	return result, nil
}

type courseMeta struct {
	lifecycle, candidateState string
}

func (r *Repository) courseMetadata(ctx context.Context, locale identity.Locale, courseIDs []string) (map[string]courseMeta, error) {
	metadata := make(map[string]courseMeta, len(courseIDs))
	if len(courseIDs) == 0 {
		return metadata, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, c.lifecycle::text, COALESCE(candidate.state::text, '')
		  FROM courses c
		  LEFT JOIN LATERAL (
			SELECT cr.state FROM course_revisions cr
			 WHERE cr.course_id = c.id AND cr.state IN ('DRAFT', 'PENDING_REVIEW', 'CHANGES_REQUESTED')
			 ORDER BY cr.revision_number DESC LIMIT 1
		  ) candidate ON TRUE
		 WHERE c.id = ANY($1::uuid[])`, courseIDs)
	if err != nil {
		return nil, fmt.Errorf("querying course lifecycle metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, lifecycle, candidateState string
		if err := rows.Scan(&id, &lifecycle, &candidateState); err != nil {
			return nil, fmt.Errorf("scanning course lifecycle metadata: %w", err)
		}
		metadata[id] = courseMeta{lifecycle: lifecycle, candidateState: candidateState}
	}
	return metadata, rows.Err()
}

func localizedTitle(locale identity.Locale, arabic, english string) string {
	if locale == identity.LocaleArabic && strings.TrimSpace(arabic) != "" {
		return arabic
	}
	if strings.TrimSpace(english) != "" {
		return english
	}
	return arabic
}

func (r *Repository) readDevices(ctx context.Context, accountID string) (identity.AdminDeviceOverview, error) {
	if r.devices == nil {
		return identity.AdminDeviceOverview{Devices: []identity.AdminDeviceView{}}, nil
	}
	return r.devices.AdminOverview(ctx, accountID, time.Now().UTC())
}

func queryInstructorUser360(ctx context.Context, tx pgx.Tx, locale identity.Locale, identityView User360Identity) (InstructorUser360, error) {
	result := InstructorUser360{StaffStatus: string(identityView.Status), OwnedCourses: []InstructorCourse{}, AuditEvents: []AuditEvent{}, Notes: []AdminNote{}}
	rows, err := tx.Query(ctx, `
		SELECT c.id::text, COALESCE(live.title_ar, draft.title_ar), COALESCE(live.title_en, draft.title_en),
		       c.lifecycle::text, COALESCE(candidate.state::text, ''), COUNT(DISTINCT e.id)
		  FROM courses c
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		  LEFT JOIN LATERAL (
			SELECT cr.state FROM course_revisions cr
			 WHERE cr.course_id = c.id AND cr.state IN ('DRAFT', 'PENDING_REVIEW', 'CHANGES_REQUESTED')
			 ORDER BY cr.revision_number DESC LIMIT 1
		  ) candidate ON TRUE
		  LEFT JOIN enrollments e ON e.course_id = c.id
		 WHERE c.owner_account_id = $1::uuid
		 GROUP BY c.id, live.title_ar, live.title_en, draft.title_ar, draft.title_en, c.lifecycle, candidate.state
		 ORDER BY c.updated_at DESC, c.id DESC
		 LIMIT $2`, identityView.ID, user360CourseLimit)
	if err != nil {
		return InstructorUser360{}, fmt.Errorf("querying instructor courses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var course InstructorCourse
		var titleAr, titleEn string
		if err := rows.Scan(&course.ID, &titleAr, &titleEn, &course.Lifecycle, &course.CandidateRevisionState, &course.EnrollmentsCount); err != nil {
			return InstructorUser360{}, fmt.Errorf("scanning instructor course: %w", err)
		}
		course.Title = localizedTitle(locale, titleAr, titleEn)
		result.OwnedCourses = append(result.OwnedCourses, course)
	}
	if err := rows.Err(); err != nil {
		return InstructorUser360{}, fmt.Errorf("iterating instructor courses: %w", err)
	}
	result.AuditEvents, err = queryTargetAuditEvents(ctx, tx, identityView.ID, user360AuditLimit)
	if err != nil {
		return InstructorUser360{}, err
	}
	result.Notes, err = queryNotes(ctx, tx, identityView.ID, user360NotesLimit)
	if err != nil {
		return InstructorUser360{}, err
	}
	return result, nil
}

func queryTargetAuditEvents(ctx context.Context, tx pgx.Tx, accountID string, limit int) ([]AuditEvent, error) {
	return queryAuditEvents(ctx, tx, AuditEventRequest{Page: 1, Limit: limit},
		"ae.target_type = 'ACCOUNT' AND ae.target_id = $1", []any{accountID})
}

func queryNotes(ctx context.Context, tx pgx.Tx, accountID string, limit int) ([]AdminNote, error) {
	rows, err := tx.Query(ctx, `
		SELECT n.id::text, COALESCE(author.display_name, author.email), n.body, n.created_at
		  FROM admin_notes n
		  JOIN accounts author ON author.id = n.author_account_id
		 WHERE n.subject_type = 'ACCOUNT' AND n.subject_account_id = $1::uuid
		 ORDER BY n.created_at DESC, n.id DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying account notes: %w", err)
	}
	defer rows.Close()
	result := make([]AdminNote, 0, limit)
	for rows.Next() {
		var note AdminNote
		if err := rows.Scan(&note.ID, &note.AuthorName, &note.Body, &note.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning account note: %w", err)
		}
		result = append(result, note)
	}
	return result, rows.Err()
}

func querySecurityEvents(ctx context.Context, tx pgx.Tx, accountID string, page, limit int) ([]SecurityEvent, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, occurred_at, event_type, request_id, evidence
		  FROM identity_security_events
		 WHERE account_id = $1::uuid
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $2 OFFSET $3`, accountID, limit, (page-1)*limit)
	if err != nil {
		return nil, fmt.Errorf("querying account security events: %w", err)
	}
	defer rows.Close()
	result := make([]SecurityEvent, 0, limit)
	for rows.Next() {
		var event SecurityEvent
		var evidence []byte
		if err := rows.Scan(&event.ID, &event.OccurredAt, &event.EventType, &event.RequestID, &evidence); err != nil {
			return nil, fmt.Errorf("scanning account security event: %w", err)
		}
		event.Evidence = map[string]any{}
		if len(evidence) > 0 {
			if err := json.Unmarshal(evidence, &event.Evidence); err != nil {
				return nil, fmt.Errorf("decoding account security event: %w", err)
			}
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
