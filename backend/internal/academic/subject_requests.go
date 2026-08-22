package academic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type SubjectRequestStatus string

const (
	SubjectRequestPending        SubjectRequestStatus = "PENDING"
	SubjectRequestApprovedNew    SubjectRequestStatus = "APPROVED_NEW"
	SubjectRequestLinkedExisting SubjectRequestStatus = "LINKED_EXISTING"
	SubjectRequestRejected       SubjectRequestStatus = "REJECTED"
	SubjectRequestCancelled      SubjectRequestStatus = "CANCELLED"
)

func (s SubjectRequestStatus) Valid() bool {
	switch s {
	case SubjectRequestPending, SubjectRequestApprovedNew, SubjectRequestLinkedExisting,
		SubjectRequestRejected, SubjectRequestCancelled:
		return true
	default:
		return false
	}
}

var (
	ErrSubjectRequestPendingExists  = errors.New("this course already has a pending subject request")
	ErrSubjectRequestNotPending     = errors.New("subject request is no longer pending")
	ErrSubjectRequestCourseInvalid  = errors.New("course is not eligible for a subject request")
	ErrSubjectRequestOwnerMismatch  = errors.New("the requester does not own the attached course")
	ErrSubjectRequestInstructorOnly = errors.New("subject requests require an active instructor")
	ErrSubjectRequestRejectReason   = errors.New("a rejection reason is required")
)

type SubjectRequestCourseConflictError struct {
	Request *SubjectRequest
}

func (e *SubjectRequestCourseConflictError) Error() string {
	return "the course already has a different subject; the request was resolved without changing the course"
}

type SubjectRequest struct {
	ID                   string               `json:"id"`
	RequesterAccountID   string               `json:"requester_account_id"`
	InstitutionID        string               `json:"institution_id"`
	CourseID             *string              `json:"course_id,omitempty"`
	ProposedTitleAr      string               `json:"proposed_title_ar"`
	ProposedTitleEn      string               `json:"proposed_title_en"`
	ProposedOfficialCode *string              `json:"proposed_official_code,omitempty"`
	AcademicContext      *string              `json:"academic_context,omitempty"`
	Note                 *string              `json:"note,omitempty"`
	Status               SubjectRequestStatus `json:"status"`
	ResolvedSubjectID    *string              `json:"resolved_subject_id,omitempty"`
	ResolutionReason     *string              `json:"resolution_reason,omitempty"`
	ResolvedByAccountID  *string              `json:"resolved_by_account_id,omitempty"`
	ResolvedAt           *time.Time           `json:"resolved_at,omitempty"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`

	RequesterDisplayName   string  `json:"requester_display_name"`
	InstitutionNameAr      string  `json:"institution_name_ar"`
	InstitutionNameEn      string  `json:"institution_name_en"`
	CourseTitleAr          *string `json:"course_title_ar,omitempty"`
	CourseTitleEn          *string `json:"course_title_en,omitempty"`
	ResolvedOfficialCode   *string `json:"resolved_official_code,omitempty"`
	ResolvedSubjectTitleAr *string `json:"resolved_subject_title_ar,omitempty"`
	ResolvedSubjectTitleEn *string `json:"resolved_subject_title_en,omitempty"`
}

type CreateSubjectRequestWorkflow struct {
	RequesterAccountID   string
	ActorDescriptor      string
	InstitutionID        string
	CourseID             *string
	ProposedOfficialCode *string
	ProposedTitleAr      string
	ProposedTitleEn      string
	AcademicContext      *string
	Note                 *string
}

type ListSubjectRequestsRequest struct {
	RequesterAccountID string
	CourseID           *string
	Status             *SubjectRequestStatus
}

type LinkSubjectRequest struct {
	Actor     Actor
	RequestID string
	SubjectID string
}

type ApproveSubjectRequestAsNew struct {
	Actor     Actor
	RequestID string
}

type RejectSubjectRequest struct {
	Actor     Actor
	RequestID string
	Reason    string
}

type subjectRequestRow struct {
	ID                   string
	RequesterAccountID   string
	InstitutionID        string
	CourseID             *string
	ProposedTitleAr      string
	ProposedTitleEn      string
	ProposedOfficialCode *string
	AcademicContext      *string
	Note                 *string
	Status               SubjectRequestStatus
}

const subjectRequestProjection = `
	SELECT sr.id::text, sr.requester_account_id::text, sr.institution_id::text,
	       sr.course_id::text, sr.proposed_title_ar, sr.proposed_title_en,
	       sr.proposed_official_code, sr.academic_context, sr.note,
	       sr.status::text, sr.resolved_subject_id::text, sr.resolution_reason,
	       sr.resolved_by_account_id::text, sr.resolved_at, sr.created_at, sr.updated_at,
	       requester.display_name, institution.name_ar, institution.name_en,
	       course_revision.title_ar, course_revision.title_en,
	       resolved.official_code, resolved.title_ar, resolved.title_en
	FROM subject_requests sr
	JOIN accounts requester ON requester.id = sr.requester_account_id
	JOIN institutions institution ON institution.id = sr.institution_id
	LEFT JOIN LATERAL (
		SELECT revision.title_ar, revision.title_en
		FROM course_revisions revision
		WHERE revision.course_id = sr.course_id
		ORDER BY
		  CASE revision.state
		    WHEN 'DRAFT' THEN 0 WHEN 'CHANGES_REQUESTED' THEN 1
		    WHEN 'PENDING_REVIEW' THEN 2 WHEN 'APPROVED' THEN 3 ELSE 4
		  END,
		  revision.revision_number DESC
		LIMIT 1
	) course_revision ON TRUE
	LEFT JOIN subjects resolved ON resolved.id = sr.resolved_subject_id`

func scanSubjectRequest(row pgx.Row) (*SubjectRequest, error) {
	var request SubjectRequest
	if err := row.Scan(
		&request.ID, &request.RequesterAccountID, &request.InstitutionID,
		&request.CourseID, &request.ProposedTitleAr, &request.ProposedTitleEn,
		&request.ProposedOfficialCode, &request.AcademicContext, &request.Note,
		&request.Status, &request.ResolvedSubjectID, &request.ResolutionReason,
		&request.ResolvedByAccountID, &request.ResolvedAt, &request.CreatedAt, &request.UpdatedAt,
		&request.RequesterDisplayName, &request.InstitutionNameAr, &request.InstitutionNameEn,
		&request.CourseTitleAr, &request.CourseTitleEn,
		&request.ResolvedOfficialCode, &request.ResolvedSubjectTitleAr, &request.ResolvedSubjectTitleEn,
	); err != nil {
		return nil, err
	}
	return &request, nil
}

type subjectRequestAudit struct {
	accountID  string
	role       string
	descriptor string
	action     string
	requestID  string
	reason     string
	metadata   map[string]any
}

func writeSubjectRequestAudit(ctx context.Context, tx pgx.Tx, audit subjectRequestAudit) error {
	if strings.TrimSpace(audit.accountID) == "" || strings.TrimSpace(audit.descriptor) == "" {
		return errors.New("subject request audit actor is required")
	}
	payload, err := json.Marshal(audit.metadata)
	if err != nil {
		return fmt.Errorf("marshaling subject request audit: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES ($1::uuid, $2, $3, $4, 'CATALOG_AND_AUTHORING',
			'SUBJECT_REQUEST', $5, $6, $7::jsonb)`,
		audit.accountID, audit.role, audit.descriptor, audit.action, audit.requestID, audit.reason, payload)
	if err != nil {
		return fmt.Errorf("writing subject request audit: %w", err)
	}
	return nil
}

func (r *Repository) CreateSubjectRequest(
	ctx context.Context,
	req CreateSubjectRequestWorkflow,
) (*SubjectRequest, error) {
	if strings.TrimSpace(req.RequesterAccountID) == "" || strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrInvalidInput
	}
	if err := validateBilingualName(req.ProposedTitleAr, req.ProposedTitleEn); err != nil {
		return nil, err
	}
	code := trimmedOrNil(req.ProposedOfficialCode)
	if code != nil && (len(*code) > 40 || NormalizeCode(*code) == "") {
		return nil, ErrInvalidInput
	}
	courseID := trimmedOrNil(req.CourseID)
	academicContext := trimmedOrNil(req.AcademicContext)
	note := trimmedOrNil(req.Note)
	descriptor := strings.TrimSpace(req.ActorDescriptor)
	if descriptor == "" {
		descriptor = req.RequesterAccountID
	}

	var createdID string
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		var role, status string
		if err := tx.QueryRow(ctx,
			`SELECT role, status FROM accounts WHERE id = $1::uuid FOR SHARE`,
			req.RequesterAccountID,
		).Scan(&role, &status); err != nil {
			return ErrSubjectRequestInstructorOnly
		}
		if role != "INSTRUCTOR" || status != "ACTIVE" {
			return ErrSubjectRequestInstructorOnly
		}
		institution, err := lockInstitution(ctx, tx, req.InstitutionID)
		if err != nil {
			return err
		}
		if institution.RetiredAt != nil {
			return ErrRetired
		}
		if courseID != nil {
			var ownerID, lifecycle, model, institutionID string
			var subjectID, liveRevisionID *string
			err := tx.QueryRow(ctx, `
				SELECT owner_account_id::text, lifecycle::text, classification_model::text,
				       institution_id::text, subject_id::text, live_revision_id::text
				FROM courses WHERE id = $1::uuid FOR UPDATE`, *courseID,
			).Scan(&ownerID, &lifecycle, &model, &institutionID, &subjectID, &liveRevisionID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSubjectRequestCourseInvalid
			}
			if err != nil {
				return fmt.Errorf("locking requested Course: %w", err)
			}
			if ownerID != req.RequesterAccountID {
				return ErrSubjectRequestOwnerMismatch
			}
			if lifecycle != "DRAFT" || model != "ACADEMIC_CATALOG" || institutionID != req.InstitutionID ||
				subjectID != nil || liveRevisionID != nil {
				return ErrSubjectRequestCourseInvalid
			}
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO subject_requests (
				requester_account_id, institution_id, course_id,
				proposed_title_ar, proposed_title_en, proposed_official_code,
				academic_context, note
			) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8)
			RETURNING id::text`,
			req.RequesterAccountID, req.InstitutionID, courseID,
			strings.TrimSpace(req.ProposedTitleAr), strings.TrimSpace(req.ProposedTitleEn), code,
			academicContext, note,
		).Scan(&createdID)
		if err != nil {
			if pgErr := pgErrorOf(err); pgErr != nil &&
				pgErr.ConstraintName == "subject_requests_one_pending_per_course" {
				return ErrSubjectRequestPendingExists
			}
			return classifyConstraint(err)
		}
		return writeSubjectRequestAudit(ctx, tx, subjectRequestAudit{
			accountID: req.RequesterAccountID, role: "INSTRUCTOR", descriptor: descriptor,
			action: "SUBJECT_REQUEST_CREATED", requestID: createdID,
			reason: "Instructor requested a missing canonical Subject",
			metadata: map[string]any{
				"institution_id":  req.InstitutionID,
				"attached_course": courseID != nil,
				"has_code":        code != nil,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return r.getSubjectRequest(ctx, createdID)
}

func (r *Repository) getSubjectRequest(ctx context.Context, requestID string) (*SubjectRequest, error) {
	request, err := scanSubjectRequest(r.pool.QueryRow(ctx,
		subjectRequestProjection+` WHERE sr.id = $1::uuid`, requestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading subject request: %w", err)
	}
	return request, nil
}

func (r *Repository) getSubjectRequestTx(ctx context.Context, tx pgx.Tx, requestID string) (*SubjectRequest, error) {
	request, err := scanSubjectRequest(tx.QueryRow(ctx,
		subjectRequestProjection+` WHERE sr.id = $1::uuid`, requestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading subject request: %w", err)
	}
	return request, nil
}

func (r *Repository) ListSubjectRequests(
	ctx context.Context,
	req ListSubjectRequestsRequest,
) ([]SubjectRequest, error) {
	if req.Status != nil && !req.Status.Valid() {
		return nil, ErrInvalidInput
	}
	query := subjectRequestProjection + ` WHERE 1=1`
	args := []any{}
	if req.RequesterAccountID != "" {
		args = append(args, req.RequesterAccountID)
		query += fmt.Sprintf(` AND sr.requester_account_id = $%d::uuid`, len(args))
	}
	if req.CourseID != nil && strings.TrimSpace(*req.CourseID) != "" {
		args = append(args, strings.TrimSpace(*req.CourseID))
		query += fmt.Sprintf(` AND sr.course_id = $%d::uuid`, len(args))
	}
	if req.Status != nil {
		args = append(args, *req.Status)
		query += fmt.Sprintf(` AND sr.status = $%d`, len(args))
	}
	query += ` ORDER BY sr.created_at DESC, sr.id DESC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing subject requests: %w", err)
	}
	defer rows.Close()
	requests := []SubjectRequest{}
	for rows.Next() {
		request, err := scanSubjectRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning subject request: %w", err)
		}
		requests = append(requests, *request)
	}
	return requests, rows.Err()
}

func lockPendingSubjectRequest(ctx context.Context, tx pgx.Tx, requestID string) (*subjectRequestRow, error) {
	var request subjectRequestRow
	err := tx.QueryRow(ctx, `
		SELECT id::text, requester_account_id::text, institution_id::text, course_id::text,
		       proposed_title_ar, proposed_title_en, proposed_official_code,
		       academic_context, note, status::text
		FROM subject_requests WHERE id = $1::uuid FOR UPDATE`, requestID,
	).Scan(&request.ID, &request.RequesterAccountID, &request.InstitutionID, &request.CourseID,
		&request.ProposedTitleAr, &request.ProposedTitleEn, &request.ProposedOfficialCode,
		&request.AcademicContext, &request.Note, &request.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking subject request: %w", err)
	}
	if request.Status != SubjectRequestPending {
		return nil, ErrSubjectRequestNotPending
	}
	return &request, nil
}

// attachResolvedSubject uses the locked Course plus an explicit NULL predicate.
// A concurrent Instructor choice can therefore never be overwritten.
func attachResolvedSubject(
	ctx context.Context,
	tx pgx.Tx,
	request *subjectRequestRow,
	subjectID string,
) (bool, error) {
	if request.CourseID == nil {
		return false, nil
	}
	var ownerID, lifecycle, model, institutionID string
	var currentSubjectID, liveRevisionID *string
	err := tx.QueryRow(ctx, `
		SELECT owner_account_id::text, lifecycle::text, classification_model::text, institution_id::text,
		       subject_id::text, live_revision_id::text
		FROM courses WHERE id = $1::uuid FOR UPDATE`, *request.CourseID,
	).Scan(&ownerID, &lifecycle, &model, &institutionID, &currentSubjectID, &liveRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrSubjectRequestCourseInvalid
	}
	if err != nil {
		return false, fmt.Errorf("locking request Course for resolution: %w", err)
	}
	if ownerID != request.RequesterAccountID || lifecycle != "DRAFT" || model != "ACADEMIC_CATALOG" ||
		institutionID != request.InstitutionID || liveRevisionID != nil {
		return false, ErrSubjectRequestCourseInvalid
	}
	if currentSubjectID != nil {
		return *currentSubjectID != subjectID, nil
	}
	command, err := tx.Exec(ctx, `
		UPDATE courses SET subject_id = $1::uuid, updated_at = now()
		WHERE id = $2::uuid
		  AND subject_id IS NULL
		  AND live_revision_id IS NULL
		  AND classification_model = 'ACADEMIC_CATALOG'`, subjectID, *request.CourseID)
	if err != nil {
		return false, classifyConstraint(err)
	}
	if command.RowsAffected() != 1 {
		return false, ErrSubjectRequestCourseInvalid
	}
	return false, nil
}

type subjectRequestResolution struct {
	actor            actor
	request          *subjectRequestRow
	status           SubjectRequestStatus
	subjectID        string
	resolutionReason *string
	action           string
	auditReason      string
}

func (r *Repository) resolveSubjectRequestRow(
	ctx context.Context,
	tx pgx.Tx,
	resolution subjectRequestResolution,
) (*SubjectRequest, bool, error) {
	conflict, err := attachResolvedSubject(ctx, tx, resolution.request, resolution.subjectID)
	if err != nil {
		return nil, false, err
	}
	if conflict {
		reason := "The attached Course already has a different Subject; it was not reassigned."
		resolution.resolutionReason = &reason
	}
	if _, err := tx.Exec(ctx, `
		UPDATE subject_requests
		SET status = $1, resolved_subject_id = $2::uuid,
		    resolution_reason = $3, resolved_by_account_id = $4::uuid,
		    resolved_at=now(), updated_at = now()
		WHERE id = $5::uuid AND status = 'PENDING'`,
		resolution.status, resolution.subjectID, resolution.resolutionReason,
		resolution.actor.AccountID, resolution.request.ID); err != nil {
		return nil, false, fmt.Errorf("resolving subject request: %w", err)
	}
	if err := writeSubjectRequestAudit(ctx, tx, subjectRequestAudit{
		accountID: resolution.actor.AccountID, role: "ADMIN", descriptor: resolution.actor.descriptor(),
		action: resolution.action, requestID: resolution.request.ID, reason: resolution.auditReason,
		metadata: map[string]any{
			"institution_id":  resolution.request.InstitutionID,
			"course_attached": resolution.request.CourseID != nil,
			"course_conflict": conflict,
		},
	}); err != nil {
		return nil, false, err
	}
	resolved, err := r.getSubjectRequestTx(ctx, tx, resolution.request.ID)
	return resolved, conflict, err
}

func (r *Repository) LinkSubjectRequest(
	ctx context.Context,
	req LinkSubjectRequest,
) (*SubjectRequest, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	var resolved *SubjectRequest
	var courseConflict bool
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		request, err := lockPendingSubjectRequest(ctx, tx, req.RequestID)
		if err != nil {
			return err
		}
		subject, err := lockSubject(ctx, tx, req.SubjectID)
		if err != nil {
			return err
		}
		if subject.RetiredAt != nil {
			return ErrRetired
		}
		if subject.InstitutionID != request.InstitutionID {
			return ErrCrossInstitution
		}
		resolved, courseConflict, err = r.resolveSubjectRequestRow(ctx, tx, subjectRequestResolution{
			actor: act, request: request, status: SubjectRequestLinkedExisting, subjectID: subject.ID,
			action:      "SUBJECT_REQUEST_LINKED_EXISTING",
			auditReason: "Admin linked the request to an existing canonical Subject",
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if courseConflict {
		return resolved, &SubjectRequestCourseConflictError{Request: resolved}
	}
	return resolved, nil
}

func (r *Repository) ApproveSubjectRequestAsNew(
	ctx context.Context,
	req ApproveSubjectRequestAsNew,
) (*SubjectRequest, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	var pending *subjectRequestRow
	var resolved *SubjectRequest
	var courseConflict bool
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		var err error
		pending, err = lockPendingSubjectRequest(ctx, tx, req.RequestID)
		if err != nil {
			return err
		}
		createReq := CreateSubjectRequest{
			Actor: req.Actor, InstitutionID: pending.InstitutionID,
			OfficialCode: pending.ProposedOfficialCode,
			TitleAr:      pending.ProposedTitleAr, TitleEn: pending.ProposedTitleEn,
		}
		command, err := validateCreateSubjectRequest(createReq)
		if err != nil {
			return err
		}
		subject, err := r.createSubjectTx(ctx, tx, command)
		if err != nil {
			return err
		}
		resolved, courseConflict, err = r.resolveSubjectRequestRow(ctx, tx, subjectRequestResolution{
			actor: act, request: pending, status: SubjectRequestApprovedNew, subjectID: subject.ID,
			action:      "SUBJECT_REQUEST_APPROVED_NEW",
			auditReason: "Admin approved the request as a new canonical Subject",
		})
		return err
	})
	if err != nil {
		if pending != nil {
			return nil, r.describeSubjectConflict(ctx, err, pending.InstitutionID,
				pending.ProposedOfficialCode, pending.ProposedTitleAr, pending.ProposedTitleEn)
		}
		return nil, err
	}
	if courseConflict {
		return resolved, &SubjectRequestCourseConflictError{Request: resolved}
	}
	return resolved, nil
}

func (r *Repository) RejectSubjectRequest(
	ctx context.Context,
	req RejectSubjectRequest,
) (*SubjectRequest, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, ErrSubjectRequestRejectReason
	}
	var rejected *SubjectRequest
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		request, err := lockPendingSubjectRequest(ctx, tx, req.RequestID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE subject_requests
			SET status = 'REJECTED', resolution_reason = $1,
			    resolved_by_account_id = $2::uuid,
			    resolved_at=now(), updated_at = now()
			WHERE id = $3::uuid AND status = 'PENDING'`,
			reason, act.AccountID, request.ID); err != nil {
			return fmt.Errorf("rejecting subject request: %w", err)
		}
		if err := writeSubjectRequestAudit(ctx, tx, subjectRequestAudit{
			accountID: act.AccountID, role: "ADMIN", descriptor: act.descriptor(),
			action: "SUBJECT_REQUEST_REJECTED", requestID: request.ID, reason: reason,
			metadata: map[string]any{"institution_id": request.InstitutionID, "course_attached": request.CourseID != nil},
		}); err != nil {
			return err
		}
		rejected, err = r.getSubjectRequestTx(ctx, tx, request.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return rejected, nil
}
