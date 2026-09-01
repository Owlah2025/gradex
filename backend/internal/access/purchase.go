package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type PurchaseRequestState string

const (
	PurchaseRequestWaitingPayment    PurchaseRequestState = "WAITING_PAYMENT"
	PurchaseRequestInvitationCreated PurchaseRequestState = "INVITATION_CREATED"
	PurchaseRequestAccessGranted     PurchaseRequestState = "ACCESS_GRANTED"
	PurchaseRequestCancelled         PurchaseRequestState = "CANCELLED"
)

func (s PurchaseRequestState) Valid() bool {
	switch s {
	case PurchaseRequestWaitingPayment, PurchaseRequestInvitationCreated, PurchaseRequestAccessGranted, PurchaseRequestCancelled:
		return true
	default:
		return false
	}
}

var (
	ErrPurchaseRequestNotFound   = errors.New("purchase request not found")
	ErrCourseNotPurchasable      = errors.New("course is not available for purchase requests")
	ErrPurchaseRequestTransition = errors.New("purchase request state conflict")

	// ErrPurchaseRequesterNotEligible means the authenticated principal is not
	// a Student who may request a purchase. It is deliberately distinct from
	// "course not purchasable": the two have different recoveries, and neither
	// reveals anything the caller does not already know about itself.
	ErrPurchaseRequesterNotEligible = errors.New("account may not request a Course purchase")

	// ErrCourseAlreadyAccessible means the Student already holds active access.
	// Creating a purchase request for it would produce an operational task
	// nobody can act on and an Admin queue entry that is already satisfied.
	ErrCourseAlreadyAccessible = errors.New("course access is already active")
)

// PurchaseRequest stores facts Gradex knows about an external/manual sale. It
// intentionally contains no payment instrument, gateway status, or payment
// provider reference.
type PurchaseRequest struct {
	ID                          string               `json:"id"`
	ReferenceCode               string               `json:"reference"`
	CourseID                    string               `json:"course_id"`
	Email                       string               `json:"email"`
	NormalizedEmail             string               `json:"normalized_email"`
	PriceMinorUnits             int64                `json:"price_minor_units"`
	Currency                    string               `json:"currency"`
	State                       PurchaseRequestState `json:"state"`
	InvitationID                *string              `json:"invitation_id,omitempty"`
	RequestedAt                 time.Time            `json:"requested_at"`
	PaymentConfirmedAt          *time.Time           `json:"payment_confirmed_at,omitempty"`
	InvitationCreatedAt         *time.Time           `json:"invitation_created_at,omitempty"`
	AccessGrantedAt             *time.Time           `json:"access_granted_at,omitempty"`
	CancelledAt                 *time.Time           `json:"cancelled_at,omitempty"`
	CourseTitleAr               string               `json:"-"`
	CourseTitleEn               string               `json:"-"`
	CourseTitle                 string               `json:"course_title,omitempty"`
	AccessEndsAtSnapshot        *time.Time           `json:"-"`
	PaymentConfirmedByAccountID *string              `json:"-"`
}

type CreatePurchaseRequestParams struct {
	CourseID string
	Email    string
	Now      time.Time
}

// CreateStudentPurchaseRequestParams carries no email.
//
// That absence is the point. The address recorded on a purchase request is the
// one an Admin will use to issue the invitation that eventually grants access,
// so accepting it from the browser would let any caller aim someone else's
// Course access at a mailbox they control. It is read from the authenticated
// Account inside the same transaction that writes the request.
type CreateStudentPurchaseRequestParams struct {
	CourseID         string
	StudentAccountID string
	Now              time.Time
}

type ListPurchaseRequestsFilter struct {
	Query  string
	State  *PurchaseRequestState
	Limit  int
	Offset int
}

type ConfirmPurchaseRequestParams struct {
	PurchaseRequestID string
	AdminAccountID    string
	Locale            identity.Locale
	Now               time.Time
	InvitationTTL     time.Duration
}

type ConfirmPurchaseRequestResult struct {
	PurchaseRequest PurchaseRequest `json:"purchase_request"`
	Invitation      Invitation      `json:"invitation"`
}

// CancelPurchaseRequestParams is an Admin recovery command. It never refunds
// payment; it makes an unfulfilled manual-sale request terminal and, when
// necessary, invalidates its still-pending invitation in the same transaction.
type CancelPurchaseRequestParams struct {
	PurchaseRequestID string
	AdminAccountID    string
	Now               time.Time
}

// CreatePurchaseRequest builds a request in the pre-authentication shape: an
// address with no owning Account.
//
// No route mounts it. Gradex now requires a verified Student before a purchase
// request can exist, and the public anonymous route was withdrawn with that
// change. It is retained because rows of exactly this shape are already in the
// production table, and the Admin lifecycle — confirm payment, issue the
// invitation, cancel — has to keep working on them. Reproducing that shape is
// what it is for.
func (r *Repository) CreatePurchaseRequest(ctx context.Context, params CreatePurchaseRequestParams) (PurchaseRequest, error) {
	if r == nil || r.pool == nil {
		return PurchaseRequest{}, errors.New("repository is not initialized")
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.CourseID)); err != nil {
		return PurchaseRequest{}, ErrCourseNotPurchasable
	}
	normalizedEmail, err := identity.NormalizeEmail(params.Email)
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("%w: %v", ErrInvalidEmail, err)
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("beginning purchase-request transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	request, created, err := createOrReusePurchaseRequestTx(ctx, tx, params.CourseID, strings.TrimSpace(params.Email), normalizedEmail, now)
	if err != nil {
		return PurchaseRequest{}, err
	}

	if created {
		metadata, err := json.Marshal(map[string]any{
			"course_id": request.CourseID,
			"reference": request.ReferenceCode,
		})
		if err != nil {
			return PurchaseRequest{}, fmt.Errorf("marshaling purchase-request audit metadata: %w", err)
		}
		// A public request deliberately has no claimed Account actor. The email is
		// absent from the audit payload so the audit trail is useful without turning
		// it into another copy of personal data.
		if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata
		) VALUES (
			'STUDENT', 'PUBLIC_PURCHASE_REQUEST', 'PURCHASE_REQUEST_CREATED',
			'IDENTITY_AND_ACCESS', 'PURCHASE_REQUEST', $1::uuid,
			'Course purchase request persisted before WhatsApp handoff', $2
		)
	`, request.ID, metadata); err != nil {
			return PurchaseRequest{}, fmt.Errorf("writing purchase-request audit event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return PurchaseRequest{}, fmt.Errorf("committing purchase request: %w", err)
	}
	return request, nil
}

// CreateStudentPurchaseRequest records one authenticated Student's intent to
// buy one Course.
//
// Everything the row carries is server-derived: the address comes from the
// Account, and the title, price, and currency come from the Course's live
// published revision and its current price. Nothing the caller sends beyond the
// Course identifier reaches storage, so there is no client-supplied value an
// Admin could later act on.
//
// Reuse rather than creation is the normal outcome of a repeated click. The
// partial unique index on (course_id, requester_account_id) for active states
// makes that a database guarantee rather than a race the handler has to win.
func (r *Repository) CreateStudentPurchaseRequest(
	ctx context.Context,
	params CreateStudentPurchaseRequestParams,
) (PurchaseRequest, error) {
	if r == nil || r.pool == nil {
		return PurchaseRequest{}, errors.New("repository is not initialized")
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.CourseID)); err != nil {
		return PurchaseRequest{}, ErrCourseNotPurchasable
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.StudentAccountID)); err != nil {
		return PurchaseRequest{}, ErrPurchaseRequesterNotEligible
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("beginning purchase-request transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	email, normalizedEmail, err := lockPurchaseRequesterTx(ctx, tx, params.StudentAccountID)
	if err != nil {
		return PurchaseRequest{}, err
	}
	// An entitlement that is already active makes the request meaningless. This
	// is checked inside the transaction that writes it, so a grant landing
	// concurrently cannot produce a request for a Course the Student can
	// already open.
	entitled, err := studentHasActiveAccessTx(ctx, tx, params.StudentAccountID, params.CourseID, now)
	if err != nil {
		return PurchaseRequest{}, err
	}
	if entitled {
		return PurchaseRequest{}, ErrCourseAlreadyAccessible
	}

	request, created, err := createOrReuseStudentPurchaseRequestTx(
		ctx, tx, params.CourseID, params.StudentAccountID, email, normalizedEmail, now,
	)
	if err != nil {
		return PurchaseRequest{}, err
	}

	if created {
		metadata, err := json.Marshal(map[string]any{
			"course_id": request.CourseID,
			"reference": request.ReferenceCode,
		})
		if err != nil {
			return PurchaseRequest{}, fmt.Errorf("marshaling purchase-request audit metadata: %w", err)
		}
		// The actor is now a real Account rather than an anonymous browser. The
		// email stays out of the audit payload for the same reason as before:
		// the trail is useful without a second copy of personal data.
		if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_role, actor_account_id, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES (
			'STUDENT', $3::uuid, 'AUTHENTICATED_STUDENT', 'PURCHASE_REQUEST_CREATED',
			'IDENTITY_AND_ACCESS', 'PURCHASE_REQUEST', $1::uuid,
			'Course purchase request persisted before WhatsApp handoff', $2
		)
	`, request.ID, metadata, params.StudentAccountID); err != nil {
			return PurchaseRequest{}, fmt.Errorf("writing purchase-request audit event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return PurchaseRequest{}, fmt.Errorf("committing purchase request: %w", err)
	}
	return request, nil
}

// lockPurchaseRequesterTx reads and locks the requesting Account, refusing
// anything that is not an active, verified Student.
//
// The role and status are re-read here rather than trusted from the session the
// handler authenticated: a suspension or a role change committed between
// admission and this write must take effect, and the row lock is what makes
// that ordering deterministic.
func lockPurchaseRequesterTx(
	ctx context.Context,
	tx pgx.Tx,
	studentAccountID string,
) (string, string, error) {
	var email, normalizedEmail, role, status string
	var verifiedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT email, normalized_email, role::text, status::text, email_verified_at
		  FROM accounts WHERE id = $1::uuid FOR UPDATE
	`, studentAccountID).Scan(&email, &normalizedEmail, &role, &status, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrPurchaseRequesterNotEligible
	}
	if err != nil {
		return "", "", fmt.Errorf("locking purchase requester: %w", err)
	}
	if role != "STUDENT" || status != "ACTIVE" || verifiedAt == nil {
		return "", "", ErrPurchaseRequesterNotEligible
	}
	return email, normalizedEmail, nil
}

func studentHasActiveAccessTx(
	ctx context.Context,
	tx pgx.Tx,
	studentAccountID, courseID string,
	now time.Time,
) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM entitlements
		   WHERE student_account_id = $1::uuid
		     AND course_id = $2::uuid
		     AND state = 'ACTIVE'
		     AND revoked_at IS NULL
		     AND access_ends_at > $3
		)
	`, studentAccountID, courseID, now).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("checking existing Course access: %w", err)
	}
	return active, nil
}

// createOrReuseStudentPurchaseRequestTx is the owned-request twin of
// createOrReusePurchaseRequestTx.
//
// The conflict target is the ownership index rather than the email index. Both
// exist: the email index still covers the historical anonymous rows, and for a
// new request the two can never disagree because the email is derived from the
// same Account the ownership column names.
func createOrReuseStudentPurchaseRequestTx(
	ctx context.Context,
	tx pgx.Tx,
	courseID, studentAccountID, email, normalizedEmail string,
	now time.Time,
) (PurchaseRequest, bool, error) {
	var request PurchaseRequest
	var created bool
	err := tx.QueryRow(ctx, `
		INSERT INTO purchase_requests (
			id, reference_code, course_id, email, normalized_email, requester_account_id,
			course_title_ar, course_title_en, price_minor_units, currency, state,
			requested_at, created_at, updated_at
		)
		SELECT
			$1::uuid,
			'GRX-' || upper(substr(replace(gen_random_uuid()::text, '-', ''), 1, 16)),
			c.id, $2, $3, $6::uuid,
			cr.title_ar, cr.title_en, price.new_value_minor_units, 'KWD', 'WAITING_PAYMENT',
			$4, $4, $4
		  FROM courses c
		  JOIN course_revisions cr ON cr.id = c.live_revision_id
		  JOIN LATERAL (
			SELECT new_value_minor_units
			  FROM course_price_changes
			 WHERE course_id = c.id AND section_id IS NULL
			 ORDER BY changed_at DESC, id DESC
			 LIMIT 1
		  ) price ON TRUE
		 WHERE c.id = $5::uuid AND `+catalogpublic.PublishedOnly("c", "cr")+`
		ON CONFLICT (course_id, requester_account_id)
			WHERE requester_account_id IS NOT NULL
			  AND state IN ('WAITING_PAYMENT', 'INVITATION_CREATED')
		DO UPDATE SET updated_at = purchase_requests.updated_at
		RETURNING (xmax = 0) AS created, id::text, reference_code, course_id::text, email, normalized_email,
		          price_minor_units, currency, state, invitation_id::text,
		          requested_at, payment_confirmed_at, invitation_created_at,
		          access_granted_at, cancelled_at, course_title_ar, course_title_en,
		          access_ends_at_snapshot, payment_confirmed_by_account_id::text
	`, uuid.NewString(), email, normalizedEmail, now, courseID, studentAccountID).Scan(
		&created, &request.ID, &request.ReferenceCode, &request.CourseID, &request.Email, &request.NormalizedEmail,
		&request.PriceMinorUnits, &request.Currency, &request.State, &request.InvitationID,
		&request.RequestedAt, &request.PaymentConfirmedAt, &request.InvitationCreatedAt,
		&request.AccessGrantedAt, &request.CancelledAt, &request.CourseTitleAr, &request.CourseTitleEn,
		&request.AccessEndsAtSnapshot, &request.PaymentConfirmedByAccountID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseRequest{}, false, ErrCourseNotPurchasable
	}
	if err != nil {
		// A historical anonymous row for the same Course and address still
		// occupies the email uniqueness index. Reusing it is the honest
		// outcome: it is the same person asking for the same Course, and
		// creating a second WAITING_PAYMENT row would put two tasks in the
		// Admin queue for one sale.
		var constraintErr *pgconn.PgError
		if errors.As(err, &constraintErr) && constraintErr.ConstraintName == "purchase_requests_one_active_course_email" {
			existing, adoptErr := adoptExistingPurchaseRequestTx(ctx, tx, courseID, normalizedEmail, studentAccountID)
			if adoptErr != nil {
				return PurchaseRequest{}, false, adoptErr
			}
			return existing, false, nil
		}
		return PurchaseRequest{}, false, fmt.Errorf("creating purchase request: %w", err)
	}
	return request, created, nil
}

// adoptExistingPurchaseRequestTx attaches ownership to an active request that
// predates authentication.
//
// It only ever runs for a row whose normalized email is the one this
// transaction just read off the authenticated Account, so the attribution it
// records is evidence rather than a guess.
func adoptExistingPurchaseRequestTx(
	ctx context.Context,
	tx pgx.Tx,
	courseID, normalizedEmail, studentAccountID string,
) (PurchaseRequest, error) {
	row := tx.QueryRow(ctx, `
		UPDATE purchase_requests
		   SET requester_account_id = COALESCE(requester_account_id, $3::uuid)
		 WHERE course_id = $1::uuid
		   AND normalized_email = $2
		   AND state IN ('WAITING_PAYMENT', 'INVITATION_CREATED')
		RETURNING id::text, reference_code, course_id::text, email, normalized_email,
		          price_minor_units, currency, state, invitation_id::text,
		          requested_at, payment_confirmed_at, invitation_created_at,
		          access_granted_at, cancelled_at, course_title_ar, course_title_en,
		          access_ends_at_snapshot, payment_confirmed_by_account_id::text
	`, courseID, normalizedEmail, studentAccountID)
	request, err := scanPurchaseRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseRequest{}, ErrPurchaseRequestNotFound
	}
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("adopting existing purchase request: %w", err)
	}
	return request, nil
}

func createOrReusePurchaseRequestTx(
	ctx context.Context,
	tx pgx.Tx,
	courseID, email, normalizedEmail string,
	now time.Time,
) (PurchaseRequest, bool, error) {
	var request PurchaseRequest
	var created bool
	err := tx.QueryRow(ctx, `
		INSERT INTO purchase_requests (
			id, reference_code, course_id, email, normalized_email,
			course_title_ar, course_title_en, price_minor_units, currency, state,
			requested_at, created_at, updated_at
		)
		SELECT
			$1::uuid,
			'GRX-' || upper(substr(replace(gen_random_uuid()::text, '-', ''), 1, 16)),
			c.id, $2, $3,
			cr.title_ar, cr.title_en, price.new_value_minor_units, 'KWD', 'WAITING_PAYMENT',
			$4, $4, $4
		  FROM courses c
		  JOIN course_revisions cr ON cr.id = c.live_revision_id
		  JOIN LATERAL (
			SELECT new_value_minor_units
			  FROM course_price_changes
			 WHERE course_id = c.id AND section_id IS NULL
			 ORDER BY changed_at DESC, id DESC
			 LIMIT 1
		  ) price ON TRUE
		 WHERE c.id = $5::uuid AND `+catalogpublic.PublishedOnly("c", "cr")+`
		ON CONFLICT (course_id, normalized_email)
			WHERE state IN ('WAITING_PAYMENT', 'INVITATION_CREATED')
		DO UPDATE SET updated_at = purchase_requests.updated_at
		RETURNING (xmax = 0) AS created, id::text, reference_code, course_id::text, email, normalized_email,
		          price_minor_units, currency, state, invitation_id::text,
		          requested_at, payment_confirmed_at, invitation_created_at,
		          access_granted_at, cancelled_at, course_title_ar, course_title_en,
		          access_ends_at_snapshot, payment_confirmed_by_account_id::text
	`, uuid.NewString(), email, normalizedEmail, now, courseID).Scan(
		&created, &request.ID, &request.ReferenceCode, &request.CourseID, &request.Email, &request.NormalizedEmail,
		&request.PriceMinorUnits, &request.Currency, &request.State, &request.InvitationID,
		&request.RequestedAt, &request.PaymentConfirmedAt, &request.InvitationCreatedAt,
		&request.AccessGrantedAt, &request.CancelledAt, &request.CourseTitleAr, &request.CourseTitleEn,
		&request.AccessEndsAtSnapshot, &request.PaymentConfirmedByAccountID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseRequest{}, false, ErrCourseNotPurchasable
	}
	if err != nil {
		return PurchaseRequest{}, false, fmt.Errorf("creating purchase request: %w", err)
	}
	return request, created, nil
}

func (r *Repository) ListPurchaseRequests(ctx context.Context, filter ListPurchaseRequestsFilter) ([]PurchaseRequest, int, error) {
	if r == nil || r.pool == nil {
		return nil, 0, errors.New("repository is not initialized")
	}
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := strings.TrimSpace(filter.Query)
	state := ""
	if filter.State != nil && filter.State.Valid() {
		state = string(*filter.State)
	}
	where := `($1 = '' OR p.reference_code ILIKE '%' || $1 || '%' OR p.normalized_email ILIKE '%' || $1 || '%' OR p.course_title_ar ILIKE '%' || $1 || '%' OR p.course_title_en ILIKE '%' || $1 || '%')
		AND ($2 = '' OR p.state = $2)`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM purchase_requests p WHERE `+where, query, state).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting purchase requests: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.id::text, p.reference_code, p.course_id::text, p.email, p.normalized_email,
		       p.price_minor_units, p.currency, p.state, p.invitation_id::text,
		       p.requested_at, p.payment_confirmed_at, p.invitation_created_at,
		       p.access_granted_at, p.cancelled_at, p.course_title_ar, p.course_title_en,
		       p.access_ends_at_snapshot, p.payment_confirmed_by_account_id::text
		  FROM purchase_requests p
		 WHERE `+where+`
		 ORDER BY p.requested_at DESC
		 LIMIT $3 OFFSET $4`, query, state, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing purchase requests: %w", err)
	}
	defer rows.Close()
	items := make([]PurchaseRequest, 0)
	for rows.Next() {
		request, err := scanPurchaseRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, request)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating purchase requests: %w", err)
	}
	return items, total, nil
}

func scanPurchaseRequest(row interface{ Scan(...any) error }) (PurchaseRequest, error) {
	var request PurchaseRequest
	if err := row.Scan(
		&request.ID, &request.ReferenceCode, &request.CourseID, &request.Email, &request.NormalizedEmail,
		&request.PriceMinorUnits, &request.Currency, &request.State, &request.InvitationID,
		&request.RequestedAt, &request.PaymentConfirmedAt, &request.InvitationCreatedAt,
		&request.AccessGrantedAt, &request.CancelledAt, &request.CourseTitleAr, &request.CourseTitleEn,
		&request.AccessEndsAtSnapshot, &request.PaymentConfirmedByAccountID,
	); err != nil {
		return PurchaseRequest{}, fmt.Errorf("scanning purchase request: %w", err)
	}
	return request, nil
}

func (r *Repository) ConfirmPurchaseRequest(ctx context.Context, params ConfirmPurchaseRequestParams) (ConfirmPurchaseRequestResult, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return ConfirmPurchaseRequestResult{}, errors.New("repository is not initialized")
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.PurchaseRequestID)); err != nil {
		return ConfirmPurchaseRequestResult{}, ErrPurchaseRequestNotFound
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.AdminAccountID)); err != nil {
		return ConfirmPurchaseRequestResult{}, ErrPurchaseRequestNotFound
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	locale := params.Locale
	if !locale.Valid() {
		locale = identity.LocaleArabic
	}
	ttl := params.InvitationTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ConfirmPurchaseRequestResult{}, fmt.Errorf("beginning payment confirmation: %w", err)
	}
	defer tx.Rollback(ctx)
	request, err := lockPurchaseRequest(ctx, tx, params.PurchaseRequestID)
	if err != nil {
		return ConfirmPurchaseRequestResult{}, err
	}
	if request.State == PurchaseRequestInvitationCreated || request.State == PurchaseRequestAccessGranted {
		invitation, err := getInvitationTx(ctx, tx, request.InvitationID)
		if err != nil {
			return ConfirmPurchaseRequestResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ConfirmPurchaseRequestResult{}, fmt.Errorf("committing idempotent payment confirmation: %w", err)
		}
		return ConfirmPurchaseRequestResult{PurchaseRequest: request, Invitation: invitation}, nil
	}
	if request.State != PurchaseRequestWaitingPayment {
		return ConfirmPurchaseRequestResult{}, ErrPurchaseRequestTransition
	}

	// courses.default_access_ends_at is nullable: a Course may be published and
	// purchasable before an Admin has configured its default access expiry. That
	// is an expected business state, so it is scanned as a nullable instant and
	// refused as ErrExpiryRequired rather than surfacing as an internal error.
	var defaultAccessEndsAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT c.default_access_ends_at
		  FROM courses c
		  JOIN course_revisions cr ON cr.id = c.live_revision_id
		 WHERE c.id = $1::uuid AND `+catalogpublic.PublishedOnly("c", "cr")+`
		 FOR SHARE
	`, request.CourseID).Scan(&defaultAccessEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfirmPurchaseRequestResult{}, ErrCourseNotPurchasable
	}
	if err != nil {
		return ConfirmPurchaseRequestResult{}, fmt.Errorf("locking purchasable course: %w", err)
	}
	if defaultAccessEndsAt == nil || defaultAccessEndsAt.IsZero() {
		return ConfirmPurchaseRequestResult{}, ErrExpiryRequired
	}
	expiry := *defaultAccessEndsAt
	if !expiry.After(now.UTC()) {
		return ConfirmPurchaseRequestResult{}, ErrExpiryRequired
	}

	invitation, err := r.issuePurchaseInvitationTx(ctx, tx, request, params.AdminAccountID, locale, now, now.Add(ttl))
	if err != nil {
		return ConfirmPurchaseRequestResult{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE purchase_requests
		   SET state = 'INVITATION_CREATED', payment_confirmed_by_account_id = $1::uuid,
		       payment_confirmed_at = $2, invitation_id = $3::uuid, invitation_created_at = $2,
		       access_ends_at_snapshot = $4, updated_at = $2
		 WHERE id = $5::uuid
	`, params.AdminAccountID, now, invitation.ID, expiry, request.ID)
	if err != nil {
		return ConfirmPurchaseRequestResult{}, fmt.Errorf("linking payment confirmation to invitation: %w", err)
	}
	request.State = PurchaseRequestInvitationCreated
	request.PaymentConfirmedByAccountID = &params.AdminAccountID
	request.PaymentConfirmedAt = &now
	request.InvitationID = &invitation.ID
	request.InvitationCreatedAt = &now
	request.AccessEndsAtSnapshot = &expiry

	metadata, _ := json.Marshal(map[string]any{
		"reference": request.ReferenceCode, "course_id": request.CourseID, "invitation_id": invitation.ID,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata
		) VALUES (
			$1::uuid, 'ADMIN', $1, 'PURCHASE_REQUEST_PAYMENT_CONFIRMED',
			'IDENTITY_AND_ACCESS', 'PURCHASE_REQUEST', $2::uuid,
			'External/manual payment confirmed and pre-authorized invitation issued', $3
		)
	`, params.AdminAccountID, request.ID, metadata); err != nil {
		return ConfirmPurchaseRequestResult{}, fmt.Errorf("auditing payment confirmation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ConfirmPurchaseRequestResult{}, fmt.Errorf("committing payment confirmation: %w", err)
	}
	return ConfirmPurchaseRequestResult{PurchaseRequest: request, Invitation: invitation}, nil
}

func (r *Repository) CancelPurchaseRequest(ctx context.Context, params CancelPurchaseRequestParams) (PurchaseRequest, error) {
	if r == nil || r.pool == nil || r.outboxWriter == nil {
		return PurchaseRequest{}, errors.New("repository is not initialized")
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.PurchaseRequestID)); err != nil {
		return PurchaseRequest{}, ErrPurchaseRequestNotFound
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.AdminAccountID)); err != nil {
		return PurchaseRequest{}, ErrPurchaseRequestNotFound
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("beginning purchase-request cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	request, err := lockPurchaseRequest(ctx, tx, params.PurchaseRequestID)
	if err != nil {
		return PurchaseRequest{}, err
	}
	if request.State == PurchaseRequestCancelled {
		if err := tx.Commit(ctx); err != nil {
			return PurchaseRequest{}, fmt.Errorf("committing idempotent purchase-request cancellation: %w", err)
		}
		return request, nil
	}
	if request.State != PurchaseRequestWaitingPayment && request.State != PurchaseRequestInvitationCreated {
		return PurchaseRequest{}, ErrPurchaseRequestTransition
	}
	if request.State == PurchaseRequestInvitationCreated {
		invitation, err := getInvitationForUpdateTx(ctx, tx, request.InvitationID)
		if err != nil {
			return PurchaseRequest{}, err
		}
		if invitation.State != StatePendingStudentAcceptance {
			return PurchaseRequest{}, ErrPurchaseRequestTransition
		}
		if _, err := tx.Exec(ctx, `UPDATE course_access_invitations SET state='CANCELLED', cancelled_at=$1 WHERE id=$2::uuid`, now, invitation.ID); err != nil {
			return PurchaseRequest{}, fmt.Errorf("cancelling linked purchase invitation: %w", err)
		}
		if invitation.ActionSecretID != nil {
			if _, err := tx.Exec(ctx, `UPDATE identity_action_secrets SET consumed_at=$1 WHERE id=$2::uuid AND consumed_at IS NULL`, now, *invitation.ActionSecretID); err != nil {
				return PurchaseRequest{}, fmt.Errorf("consuming linked purchase invitation secret: %w", err)
			}
		}
		metadata, _ := json.Marshal(map[string]any{"course_id": invitation.CourseID, "purchase_reference": request.ReferenceCode})
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (actor_account_id, actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata)
			VALUES ($1::uuid, 'ADMIN', $1, 'COURSE_ACCESS_INVITATION_CANCELLED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $2::uuid, 'Purchase-backed Course access invitation cancelled', $3)
		`, params.AdminAccountID, invitation.ID, metadata); err != nil {
			return PurchaseRequest{}, fmt.Errorf("auditing linked purchase invitation cancellation: %w", err)
		}
		locale := identity.LocaleArabic
		localeErr := tx.QueryRow(ctx, `SELECT locale FROM accounts WHERE normalized_email=$1`, invitation.NormalizedEmail).Scan(&locale)
		if errors.Is(localeErr, pgx.ErrNoRows) {
			localeErr = tx.QueryRow(ctx, `SELECT safe_payload->>'locale' FROM outbox_events WHERE event_type='access.invitation_issued' AND aggregate_type='COURSE_ACCESS_INVITATION' AND aggregate_id=$1::uuid ORDER BY occurred_at ASC LIMIT 1`, invitation.ID).Scan(&locale)
		}
		if localeErr != nil && !errors.Is(localeErr, pgx.ErrNoRows) {
			return PurchaseRequest{}, fmt.Errorf("resolving linked purchase invitation locale: %w", localeErr)
		}
		event := outbox.Event{ID: uuid.NewString(), Type: "access.invitation_cancelled", SchemaVersion: 1, SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "COURSE_ACCESS_INVITATION", AggregateID: invitation.ID, AggregateRevision: 2, CorrelationID: uuid.NewString(), SafePayload: map[string]any{"course_id": invitation.CourseID, "locale": string(locale), "template_contract": "course-access-invitation-cancelled-v1"}}
		if _, err := r.outboxWriter.Append(ctx, tx, event, outbox.NoticeDelivery{Destination: invitation.NormalizedEmail, Locale: string(locale), TemplateContract: "course-access-invitation-cancelled-v1"}); err != nil {
			return PurchaseRequest{}, fmt.Errorf("writing linked purchase invitation cancellation event: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchase_requests SET state='CANCELLED', cancelled_at=$1, updated_at=$1 WHERE id=$2::uuid`, now, request.ID); err != nil {
		return PurchaseRequest{}, fmt.Errorf("cancelling purchase request: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"reference": request.ReferenceCode, "course_id": request.CourseID})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (actor_account_id, actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata)
		VALUES ($1::uuid, 'ADMIN', $1, 'PURCHASE_REQUEST_CANCELLED', 'IDENTITY_AND_ACCESS', 'PURCHASE_REQUEST', $2::uuid, 'Manual purchase request cancelled', $3)
	`, params.AdminAccountID, request.ID, metadata); err != nil {
		return PurchaseRequest{}, fmt.Errorf("auditing purchase-request cancellation: %w", err)
	}
	request.State = PurchaseRequestCancelled
	request.CancelledAt = &now
	if err := tx.Commit(ctx); err != nil {
		return PurchaseRequest{}, fmt.Errorf("committing purchase-request cancellation: %w", err)
	}
	return request, nil
}

func lockPurchaseRequest(ctx context.Context, tx pgx.Tx, id string) (PurchaseRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT id::text, reference_code, course_id::text, email, normalized_email,
		       price_minor_units, currency, state, invitation_id::text,
		       requested_at, payment_confirmed_at, invitation_created_at,
		       access_granted_at, cancelled_at, course_title_ar, course_title_en,
		       access_ends_at_snapshot, payment_confirmed_by_account_id::text
		  FROM purchase_requests WHERE id = $1::uuid FOR UPDATE
	`, id)
	request, err := scanPurchaseRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseRequest{}, ErrPurchaseRequestNotFound
	}
	return request, err
}

func getInvitationTx(ctx context.Context, tx pgx.Tx, id *string) (Invitation, error) {
	if id == nil {
		return Invitation{}, ErrInvitationStateConflict
	}
	var invitation Invitation
	var secretID *string
	err := tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at, decided_at, cancelled_at
		  FROM course_access_invitations WHERE id = $1::uuid
	`, *id).Scan(
		&invitation.ID, &invitation.NormalizedEmail, &invitation.Email, &invitation.CourseID,
		&invitation.CreatedByAccountID, &invitation.DecidedByAccountID, &invitation.AcceptedByAccountID,
		&invitation.State, &invitation.DecisionReason, &invitation.AdminNote, &invitation.ExternalReference,
		&secretID, &invitation.CreatedAt, &invitation.AcceptedAt, &invitation.DecidedAt, &invitation.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	invitation.ActionSecretID = secretID
	return invitation, err
}

func getInvitationForUpdateTx(ctx context.Context, tx pgx.Tx, id *string) (Invitation, error) {
	if id == nil {
		return Invitation{}, ErrInvitationStateConflict
	}
	var invitation Invitation
	var secretID *string
	err := tx.QueryRow(ctx, `
		SELECT id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		       decided_by_account_id::text, accepted_by_account_id::text, state, decision_reason,
		       admin_note, external_reference, action_secret_id::text, created_at, accepted_at, decided_at, cancelled_at
		  FROM course_access_invitations WHERE id = $1::uuid FOR UPDATE
	`, *id).Scan(&invitation.ID, &invitation.NormalizedEmail, &invitation.Email, &invitation.CourseID,
		&invitation.CreatedByAccountID, &invitation.DecidedByAccountID, &invitation.AcceptedByAccountID,
		&invitation.State, &invitation.DecisionReason, &invitation.AdminNote, &invitation.ExternalReference,
		&secretID, &invitation.CreatedAt, &invitation.AcceptedAt, &invitation.DecidedAt, &invitation.CancelledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	invitation.ActionSecretID = secretID
	return invitation, err
}

// issuePurchaseInvitationTx deliberately emits the exact existing Course
// invitation email contract. It is transaction-bound to payment confirmation,
// so a committed confirmation can never be detached from its invitation.
func (r *Repository) issuePurchaseInvitationTx(
	ctx context.Context,
	tx pgx.Tx,
	request PurchaseRequest,
	adminAccountID string,
	locale identity.Locale,
	now, expiresAt time.Time,
) (Invitation, error) {
	var role string
	recipientLocale := locale
	err := tx.QueryRow(ctx, "SELECT role, locale FROM accounts WHERE normalized_email = $1", request.NormalizedEmail).Scan(&role, &recipientLocale)
	if err == nil && role != "STUDENT" {
		return Invitation{}, ErrIneligibleRecipient
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, fmt.Errorf("checking purchase recipient: %w", err)
	}
	rawBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawBytes); err != nil {
		return Invitation{}, fmt.Errorf("generating purchase invitation secret: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawBytes)
	digest := sha256.Sum256([]byte(token))
	secretID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity_action_secrets (id, account_id, purpose, secret_digest, issued_at, expires_at)
		VALUES ($1::uuid, NULL, 'COURSE_ACCESS_INVITATION', $2, $3, $4)
	`, secretID, digest[:], now, expiresAt); err != nil {
		return Invitation{}, fmt.Errorf("creating purchase invitation secret: %w", err)
	}
	invitationID := uuid.NewString()
	var invitation Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id, state, action_secret_id, created_at
		) VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, 'PENDING_STUDENT_ACCEPTANCE', $6::uuid, $7)
		RETURNING id::text, normalized_email, email, course_id::text, created_by_account_id::text,
		          state, action_secret_id::text, created_at
	`, invitationID, request.NormalizedEmail, request.Email, request.CourseID, adminAccountID, secretID, now).Scan(
		&invitation.ID, &invitation.NormalizedEmail, &invitation.Email, &invitation.CourseID,
		&invitation.CreatedByAccountID, &invitation.State, &invitation.ActionSecretID, &invitation.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "cai_one_non_terminal_per_pair" || pgErr.Code == "23505") {
			return Invitation{}, ErrDuplicateInvitation
		}
		return Invitation{}, fmt.Errorf("creating purchase invitation: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"purchase_reference": request.ReferenceCode, "course_id": request.CourseID})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata
		) VALUES ($1::uuid, 'ADMIN', $1, 'COURSE_ACCESS_INVITATION_ISSUED',
			'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $2::uuid,
			'Purchase-backed Course access invitation created', $3)
	`, adminAccountID, invitation.ID, metadata); err != nil {
		return Invitation{}, fmt.Errorf("auditing purchase invitation: %w", err)
	}
	event := outbox.Event{
		ID: uuid.NewString(), Type: "access.invitation_issued", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "COURSE_ACCESS_INVITATION",
		AggregateID: invitation.ID, AggregateRevision: 1, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{
			"action_secret_id": secretID, "purpose": string(identity.ActionCourseAccessInvitation),
			"course_id": request.CourseID, "secret_expires_at": expiresAt,
			"locale": string(recipientLocale), "template_contract": "course-access-invitation-v1",
			"purchase_backed": true,
		},
	}
	delivery := outbox.VerificationDelivery{
		Destination: request.NormalizedEmail, Locale: string(recipientLocale),
		TemplateContract: "course-access-invitation-v1", VerificationToken: token, ExpiresAt: expiresAt,
	}
	if _, err := r.outboxWriter.Append(ctx, tx, event, delivery); err != nil {
		return Invitation{}, fmt.Errorf("writing purchase invitation outbox event: %w", err)
	}
	return invitation, nil
}

// CompletePurchaseInvitationAcceptance performs the exceptional grant path:
// the Admin approval occurred at payment confirmation, while the matching
// Student's acceptance proves identity and consent.
func (r *Repository) CompletePurchaseInvitationAcceptance(
	ctx context.Context,
	tx pgx.Tx,
	invitation Invitation,
	studentAccountID string,
	now time.Time,
) (Invitation, bool, error) {
	request, err := lockPurchaseRequestByInvitation(ctx, tx, invitation.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, err
	}
	if request.State != PurchaseRequestInvitationCreated || request.PaymentConfirmedByAccountID == nil ||
		request.PaymentConfirmedAt == nil || request.AccessEndsAtSnapshot == nil || request.InvitationID == nil ||
		*request.InvitationID != invitation.ID || request.CourseID != invitation.CourseID {
		return Invitation{}, true, ErrPurchaseRequestTransition
	}
	var role string
	var locale identity.Locale
	if err := tx.QueryRow(ctx, `SELECT role, locale FROM accounts WHERE id = $1::uuid`, studentAccountID).Scan(&role, &locale); err != nil || role != "STUDENT" {
		return Invitation{}, true, ErrIneligibleRecipient
	}
	var activeID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM entitlements
		 WHERE student_account_id = $1::uuid AND course_id = $2::uuid AND scope_kind = 'COURSE' AND state = 'ACTIVE'
		 FOR UPDATE
	`, studentAccountID, invitation.CourseID).Scan(&activeID)
	if err == nil {
		return Invitation{}, true, ErrAlreadyHasActiveAccess
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, true, fmt.Errorf("checking purchase entitlement: %w", err)
	}
	var enrollmentID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id, created_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
		ON CONFLICT (student_account_id, course_id) DO UPDATE SET created_at = enrollments.created_at
		RETURNING id::text
	`, uuid.NewString(), studentAccountID, invitation.CourseID, now).Scan(&enrollmentID); err != nil {
		return Invitation{}, true, fmt.Errorf("creating purchase enrollment: %w", err)
	}
	var entitlement Entitlement
	var sourceID *string
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id,
			original_access_ends_at, access_ends_at, retirement_eligibility_at, state, revision, created_at, updated_at
		) VALUES (
			$1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'PURCHASE_REQUEST', $4::uuid,
			$5, $5, $6, 'ACTIVE', 1, $7, $7
		) RETURNING id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		            grant_source, source_invitation_id::text, original_access_ends_at, access_ends_at,
		            revoked_at, retirement_eligibility_at, state, revision, created_at, updated_at
	`, uuid.NewString(), studentAccountID, invitation.CourseID, invitation.ID, *request.AccessEndsAtSnapshot,
		*request.PaymentConfirmedAt, now).Scan(
		&entitlement.ID, &entitlement.StudentAccountID, &entitlement.ScopeKind, &entitlement.ScopeID,
		&entitlement.CourseID, &entitlement.GrantSource, &sourceID, &entitlement.OriginalAccessEndsAt,
		&entitlement.AccessEndsAt, &revokedAt, &entitlement.RetirementEligibilityAt, &entitlement.State,
		&entitlement.Revision, &entitlement.CreatedAt, &entitlement.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "entitlements_one_active_student_course" || pgErr.Code == "23505") {
			return Invitation{}, true, ErrAlreadyHasActiveAccess
		}
		return Invitation{}, true, fmt.Errorf("creating purchase entitlement: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE course_access_invitations
		   SET state = 'APPROVED', accepted_by_account_id = $1::uuid, accepted_at = $2,
		       decided_by_account_id = $3::uuid, decided_at = $2
		 WHERE id = $4::uuid
	`, studentAccountID, now, *request.PaymentConfirmedByAccountID, invitation.ID); err != nil {
		return Invitation{}, true, fmt.Errorf("approving purchase invitation on acceptance: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE purchase_requests
		   SET state = 'ACCESS_GRANTED', access_granted_at = $1, updated_at = $1
		 WHERE id = $2::uuid
	`, now, request.ID); err != nil {
		return Invitation{}, true, fmt.Errorf("completing purchase request: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"reference": request.ReferenceCode, "entitlement_id": entitlement.ID, "enrollment_id": enrollmentID})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata
		) VALUES
		($1::uuid, 'STUDENT', $1, 'PURCHASE_BACKED_INVITATION_ACCEPTED', 'IDENTITY_AND_ACCESS', 'COURSE_ACCESS_INVITATION', $2::uuid, 'Pre-authorized purchase invitation accepted', $3),
		($4::uuid, 'ADMIN', $4, 'ENTITLEMENT_GRANTED', 'IDENTITY_AND_ACCESS', 'ENTITLEMENT', $5::uuid, 'Purchase entitlement granted after matching Student acceptance', $3)
	`, studentAccountID, invitation.ID, metadata, *request.PaymentConfirmedByAccountID, entitlement.ID); err != nil {
		return Invitation{}, true, fmt.Errorf("auditing purchase grant: %w", err)
	}
	event := outbox.Event{ID: uuid.NewString(), Type: "access.granted", SchemaVersion: 1, SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "ENTITLEMENT", AggregateID: entitlement.ID, AggregateRevision: 1, CorrelationID: uuid.NewString(), SafePayload: map[string]any{
		"entitlement_id": entitlement.ID, "student_account_id": studentAccountID, "course_id": invitation.CourseID,
		"source_invitation_id": invitation.ID, "access_ends_at": entitlement.AccessEndsAt.UTC().Format(time.RFC3339),
		"locale": string(locale), "template_contract": "course-access-granted-v1",
	}}
	if _, err := r.outboxWriter.Append(ctx, tx, event, outbox.NoticeDelivery{Destination: invitation.NormalizedEmail, Locale: string(locale), TemplateContract: "course-access-granted-v1"}); err != nil {
		return Invitation{}, true, fmt.Errorf("writing purchase grant outbox event: %w", err)
	}
	invitation.State = StateApproved
	invitation.AcceptedByAccountID = &studentAccountID
	invitation.AcceptedAt = &now
	invitation.DecidedByAccountID = request.PaymentConfirmedByAccountID
	invitation.DecidedAt = &now
	return invitation, true, nil
}

func lockPurchaseRequestByInvitation(ctx context.Context, tx pgx.Tx, invitationID string) (PurchaseRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT id::text, reference_code, course_id::text, email, normalized_email,
		       price_minor_units, currency, state, invitation_id::text,
		       requested_at, payment_confirmed_at, invitation_created_at,
		       access_granted_at, cancelled_at, course_title_ar, course_title_en,
		       access_ends_at_snapshot, payment_confirmed_by_account_id::text
		  FROM purchase_requests WHERE invitation_id = $1::uuid FOR UPDATE
	`, invitationID)
	return scanPurchaseRequest(row)
}

func WhatsAppHandoffURL(number string, request PurchaseRequest, locale identity.Locale) (string, error) {
	if strings.TrimSpace(number) == "" {
		return "", errors.New("sales WhatsApp number is not configured")
	}
	title := request.CourseTitleEn
	if locale == identity.LocaleArabic {
		title = request.CourseTitleAr
	}
	if title == "" {
		title = request.CourseTitleEn
	}
	price := formatKWD(request.PriceMinorUnits)
	message := fmt.Sprintf("Hello, I want to buy %s on Gradex.\nPrice: %s\nEmail: %s\nRequest: %s", title, price, request.Email, request.ReferenceCode)
	if locale == identity.LocaleArabic {
		message = fmt.Sprintf("مرحبًا، أريد شراء كورس %s على Gradex.\nالسعر: %s\nالبريد الإلكتروني: %s\nرقم الطلب: %s", title, price, request.Email, request.ReferenceCode)
	}
	return "https://wa.me/" + number + "?text=" + url.QueryEscape(message), nil
}

func formatKWD(minorUnits int64) string {
	return fmt.Sprintf("%d.%03d KWD", minorUnits/1000, minorUnits%1000)
}
