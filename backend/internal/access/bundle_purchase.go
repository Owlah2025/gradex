package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// CreateStudentBundlePurchaseRequest snapshots one coherent, currently
// purchasable Bundle. The caller supplies only identities; all commercial and
// membership facts are read while the Bundle row is locked.
func (r *Repository) CreateStudentBundlePurchaseRequest(
	ctx context.Context,
	params CreateStudentBundlePurchaseRequestParams,
) (PurchaseRequest, error) {
	if r == nil || r.pool == nil {
		return PurchaseRequest{}, errors.New("repository is not initialized")
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.BundleID)); err != nil {
		return PurchaseRequest{}, ErrBundleNotPurchasable
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
		return PurchaseRequest{}, fmt.Errorf("beginning bundle purchase transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	email, normalizedEmail, err := lockPurchaseRequesterTx(ctx, tx, params.StudentAccountID)
	if err != nil {
		return PurchaseRequest{}, err
	}

	var bundleRevision int64
	var titleAr, titleEn string
	var regular int64
	var offer *int64
	err = tx.QueryRow(ctx, `
		SELECT b.revision, b.title_ar, b.title_en,
		       price.new_value_minor_units, price.offer_price_minor_units
		FROM bundles b
		JOIN LATERAL (
			SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
			WHERE bundle_id=b.id ORDER BY changed_at DESC, id DESC LIMIT 1
		) price ON TRUE
		WHERE b.id=$1::uuid AND b.lifecycle='PUBLISHED'
		  AND (SELECT count(*) FROM bundle_courses members WHERE members.bundle_id=b.id) >= 2
		  AND NOT EXISTS (
			SELECT 1 FROM bundle_courses broken
			JOIN courses c ON c.id=broken.course_id
			LEFT JOIN course_revisions cr ON cr.id=c.live_revision_id
			WHERE broken.bundle_id=b.id AND NOT (`+catalogpublic.PublishedOnly("c", "cr")+`)
		  )
		FOR SHARE OF b
	`, params.BundleID).Scan(&bundleRevision, &titleAr, &titleEn, &regular, &offer)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseRequest{}, ErrBundleNotPurchasable
	}
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("locking purchasable bundle: %w", err)
	}
	effective := regular
	if offer != nil {
		effective = *offer
	}

	var request PurchaseRequest
	var created bool
	err = tx.QueryRow(ctx, `
		INSERT INTO purchase_requests (
			id, reference_code, course_id, bundle_id, target_kind,
			email, normalized_email, requester_account_id,
			course_title_ar, course_title_en, bundle_title_ar, bundle_title_en, bundle_revision,
			price_minor_units, regular_price_minor_units, currency, state,
			requested_at, created_at, updated_at
		) VALUES (
			$1::uuid, 'GRX-' || upper(substr(replace(gen_random_uuid()::text, '-', ''), 1, 16)),
			NULL, $2::uuid, 'BUNDLE', $3, $4, $5::uuid,
			NULL, NULL, $6, $7, $8, $9, $10, 'KWD', 'WAITING_PAYMENT', $11, $11, $11
		)
		ON CONFLICT (bundle_id, requester_account_id)
			WHERE target_kind='BUNDLE' AND requester_account_id IS NOT NULL AND state='WAITING_PAYMENT'
		DO UPDATE SET updated_at=purchase_requests.updated_at
		RETURNING (xmax=0), id::text, reference_code, COALESCE(course_id::text, ''), email, normalized_email,
		          price_minor_units, currency, state, invitation_id::text,
		          requested_at, payment_confirmed_at, invitation_created_at,
		          access_granted_at, cancelled_at, COALESCE(course_title_ar, ''), COALESCE(course_title_en, ''),
		          access_ends_at_snapshot, payment_confirmed_by_account_id::text,
		          regular_price_minor_units, target_kind, bundle_id::text, bundle_revision,
		          bundle_title_ar, bundle_title_en, requester_account_id::text
	`, uuid.NewString(), params.BundleID, email, normalizedEmail, params.StudentAccountID,
		titleAr, titleEn, bundleRevision, effective, regular, now).Scan(
		&created, &request.ID, &request.ReferenceCode, &request.CourseID, &request.Email, &request.NormalizedEmail,
		&request.PriceMinorUnits, &request.Currency, &request.State, &request.InvitationID,
		&request.RequestedAt, &request.PaymentConfirmedAt, &request.InvitationCreatedAt,
		&request.AccessGrantedAt, &request.CancelledAt, &request.CourseTitleAr, &request.CourseTitleEn,
		&request.AccessEndsAtSnapshot, &request.PaymentConfirmedByAccountID,
		&request.RegularPriceMinorUnits, &request.TargetKind, &request.BundleID, &request.BundleRevision,
		&request.BundleTitleAr, &request.BundleTitleEn, &request.RequesterAccountID,
	)
	if err != nil {
		return PurchaseRequest{}, fmt.Errorf("creating bundle purchase request: %w", err)
	}
	if created {
		tag, err := tx.Exec(ctx, `
			INSERT INTO purchase_request_bundle_items (
				purchase_request_id, course_id, position, course_title_ar, course_title_en
			)
			SELECT $1::uuid, bc.course_id, bc.position, cr.title_ar, cr.title_en
			FROM bundle_courses bc
			JOIN courses c ON c.id=bc.course_id
			JOIN course_revisions cr ON cr.id=c.live_revision_id
			WHERE bc.bundle_id=$2::uuid ORDER BY bc.position, bc.course_id
		`, request.ID, params.BundleID)
		if err != nil {
			return PurchaseRequest{}, fmt.Errorf("snapshotting bundle courses: %w", err)
		}
		if tag.RowsAffected() < 2 {
			return PurchaseRequest{}, ErrBundleSnapshotInvalid
		}
		metadata, _ := json.Marshal(map[string]any{
			"bundle_id": params.BundleID, "bundle_revision": bundleRevision,
			"reference": request.ReferenceCode, "course_count": tag.RowsAffected(),
		})
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (
				actor_role, actor_account_id, actor_descriptor, action, module,
				target_type, target_id, reason, metadata
			) VALUES (
				'STUDENT', $1::uuid, 'AUTHENTICATED_STUDENT', 'BUNDLE_PURCHASE_REQUEST_CREATED',
				'IDENTITY_AND_ACCESS', 'PURCHASE_REQUEST', $2::uuid,
				'Bundle purchase request persisted before WhatsApp handoff', $3
			)
		`, params.StudentAccountID, request.ID, metadata); err != nil {
			return PurchaseRequest{}, fmt.Errorf("auditing bundle purchase request: %w", err)
		}
	}
	request.BundleItems, err = loadBundlePurchaseItems(ctx, tx, request.ID)
	if err != nil {
		return PurchaseRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseRequest{}, fmt.Errorf("committing bundle purchase request: %w", err)
	}
	return request, nil
}

func loadBundlePurchaseItems(ctx context.Context, tx pgx.Tx, requestID string) ([]BundlePurchaseItem, error) {
	rows, err := tx.Query(ctx, `
		SELECT course_id::text, position, course_title_ar, course_title_en
		FROM purchase_request_bundle_items WHERE purchase_request_id=$1::uuid
		ORDER BY position, course_id
	`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading bundle purchase snapshot: %w", err)
	}
	defer rows.Close()
	items := []BundlePurchaseItem{}
	for rows.Next() {
		var item BundlePurchaseItem
		if err := rows.Scan(&item.CourseID, &item.Position, &item.CourseTitleAr, &item.CourseTitleEn); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) confirmBundlePurchaseTx(
	ctx context.Context,
	tx pgx.Tx,
	request *PurchaseRequest,
	adminAccountID string,
	now time.Time,
) ([]BundleGrant, error) {
	if request == nil || request.BundleID == nil || request.RequesterAccountID == nil ||
		request.BundleRevision == nil || request.InvitationID != nil {
		return nil, ErrBundleSnapshotInvalid
	}
	if request.State == PurchaseRequestAccessGranted {
		return loadBundleGrants(ctx, tx, request.ID)
	}
	if request.State != PurchaseRequestWaitingPayment {
		return nil, ErrPurchaseRequestTransition
	}
	var role, status string
	var verifiedAt *time.Time
	var locale identity.Locale
	if err := tx.QueryRow(ctx, `
		SELECT role::text, status::text, email_verified_at, locale
		FROM accounts WHERE id=$1::uuid FOR UPDATE
	`, *request.RequesterAccountID).Scan(&role, &status, &verifiedAt, &locale); err != nil ||
		role != "STUDENT" || status != "ACTIVE" || verifiedAt == nil {
		return nil, ErrPurchaseRequesterNotEligible
	}

	items, err := loadBundlePurchaseItems(ctx, tx, request.ID)
	if err != nil || len(items) < 2 {
		return nil, ErrBundleSnapshotInvalid
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CourseID)
	}
	sort.Strings(ids)
	rows, err := tx.Query(ctx, `
		SELECT c.id::text, c.default_access_ends_at
		FROM courses c JOIN course_revisions cr ON cr.id=c.live_revision_id
		WHERE c.id=ANY($1::uuid[]) AND `+catalogpublic.PublishedOnly("c", "cr")+`
		ORDER BY c.id FOR SHARE OF c, cr
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("locking Bundle snapshot Courses: %w", err)
	}
	expiries := make(map[string]time.Time, len(ids))
	for rows.Next() {
		var id string
		var expiry *time.Time
		if err := rows.Scan(&id, &expiry); err != nil {
			rows.Close()
			return nil, err
		}
		if expiry == nil || !expiry.After(now.UTC()) {
			rows.Close()
			return nil, ErrExpiryRequired
		}
		expiries[id] = *expiry
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(expiries) != len(ids) {
		return nil, ErrBundleSnapshotInvalid
	}

	grants := make([]BundleGrant, 0, len(ids))
	for _, courseID := range ids {
		grant, err := grantBundleCourse(ctx, tx, request, adminAccountID, courseID, expiries[courseID], now)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	metadata, _ := json.Marshal(map[string]any{
		"reference": request.ReferenceCode, "bundle_id": *request.BundleID,
		"bundle_revision": *request.BundleRevision, "course_grants": grants,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES
		($1::uuid, 'ADMIN', $1, 'BUNDLE_PURCHASE_PAYMENT_CONFIRMED', 'IDENTITY_AND_ACCESS',
		 'PURCHASE_REQUEST', $2::uuid, 'External/manual Bundle payment confirmed', $3),
		($1::uuid, 'ADMIN', $1, 'BUNDLE_ACCESS_GRANTED', 'IDENTITY_AND_ACCESS',
		 'PURCHASE_REQUEST', $2::uuid, 'Bundle snapshot Course access granted atomically', $3)
	`, adminAccountID, request.ID, metadata); err != nil {
		return nil, fmt.Errorf("auditing Bundle grant: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE purchase_requests SET state='ACCESS_GRANTED', payment_confirmed_by_account_id=$1::uuid,
		       payment_confirmed_at=$2, access_granted_at=$2, updated_at=$2
		WHERE id=$3::uuid
	`, adminAccountID, now, request.ID); err != nil {
		return nil, fmt.Errorf("completing Bundle purchase request: %w", err)
	}
	request.State = PurchaseRequestAccessGranted
	request.PaymentConfirmedByAccountID = &adminAccountID
	request.PaymentConfirmedAt = &now
	request.AccessGrantedAt = &now

	bundleTitle := ""
	if request.BundleTitleEn != nil {
		bundleTitle = *request.BundleTitleEn
	}
	if locale == identity.LocaleArabic && request.BundleTitleAr != nil {
		bundleTitle = *request.BundleTitleAr
	}
	event := outbox.Event{
		ID: uuid.NewString(), Type: "access.bundle_granted", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "PURCHASE_REQUEST",
		AggregateID: request.ID, AggregateRevision: 1, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{
			"purchase_request_id": request.ID, "bundle_id": *request.BundleID,
			"bundle_title": bundleTitle, "course_count": len(grants),
			"locale": string(locale), "template_contract": "bundle-access-granted-v1",
		},
	}
	if _, err := r.outboxWriter.Append(ctx, tx, event, outbox.NoticeDelivery{
		Destination: request.NormalizedEmail, Locale: string(locale), TemplateContract: "bundle-access-granted-v1",
	}); err != nil {
		return nil, fmt.Errorf("writing Bundle access notification: %w", err)
	}
	return grants, nil
}

func grantBundleCourse(
	ctx context.Context,
	tx pgx.Tx,
	request *PurchaseRequest,
	adminAccountID, courseID string,
	newExpiry, now time.Time,
) (BundleGrant, error) {
	var entitlementID string
	var existingExpiry time.Time
	err := tx.QueryRow(ctx, `
		SELECT id::text, access_ends_at FROM entitlements
		WHERE student_account_id=$1::uuid AND course_id=$2::uuid
		  AND scope_kind='COURSE' AND state='ACTIVE' FOR UPDATE
	`, *request.RequesterAccountID, courseID).Scan(&entitlementID, &existingExpiry)
	if err == nil {
		return preserveOrExtendBundleEntitlement(ctx, tx, request, adminAccountID, courseID, entitlementID, existingExpiry, newExpiry, now)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return BundleGrant{}, fmt.Errorf("checking Bundle Course entitlement: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id, created_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
		ON CONFLICT (student_account_id, course_id) DO UPDATE SET created_at=enrollments.created_at
	`, uuid.NewString(), *request.RequesterAccountID, courseID, now); err != nil {
		return BundleGrant{}, fmt.Errorf("creating Bundle Course enrollment: %w", err)
	}
	entitlementID = uuid.NewString()
	tag, err := tx.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, source_purchase_request_id, original_access_ends_at,
			access_ends_at, retirement_eligibility_at, state, revision, created_at, updated_at
		) VALUES (
			$1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'BUNDLE_PURCHASE',
			NULL, $4::uuid, $5, $5, $6, 'ACTIVE', 1, $6, $6
		)
		ON CONFLICT (student_account_id, course_id)
			WHERE state='ACTIVE' AND scope_kind='COURSE' DO NOTHING
	`, entitlementID, *request.RequesterAccountID, courseID, request.ID, newExpiry, now)
	if err != nil {
		return BundleGrant{}, fmt.Errorf("creating Bundle Course entitlement: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.QueryRow(ctx, `
			SELECT id::text, access_ends_at FROM entitlements
			WHERE student_account_id=$1::uuid AND course_id=$2::uuid
			  AND scope_kind='COURSE' AND state='ACTIVE' FOR UPDATE
		`, *request.RequesterAccountID, courseID).Scan(&entitlementID, &existingExpiry); err != nil {
			return BundleGrant{}, fmt.Errorf("loading concurrent Bundle entitlement: %w", err)
		}
		return preserveOrExtendBundleEntitlement(ctx, tx, request, adminAccountID, courseID, entitlementID, existingExpiry, newExpiry, now)
	}
	grant := BundleGrant{CourseID: courseID, EntitlementID: entitlementID, Disposition: "GRANTED", ResultingAccessEndsAt: newExpiry}
	if err := insertBundleGrantProvenance(ctx, tx, request, grant, now); err != nil {
		return BundleGrant{}, err
	}
	return grant, nil
}

// preserveOrExtendBundleEntitlement is the single disposition rule for a
// snapshot Course the Student already holds. It is reached from two places —
// the entitlement found before insertion, and the one a concurrent Bundle
// confirmation inserted first — and both must decide identically, so the rule
// lives here rather than being written out at each site.
//
// A held entitlement is never replaced: an equal or later expiry is PRESERVED
// untouched, and only a strictly earlier one is EXTENDED in place, keeping the
// Student's original entitlement identity and its adjustment history.
func preserveOrExtendBundleEntitlement(
	ctx context.Context,
	tx pgx.Tx,
	request *PurchaseRequest,
	adminAccountID, courseID, entitlementID string,
	existingExpiry, newExpiry, now time.Time,
) (BundleGrant, error) {
	disposition := "PRESERVED"
	resulting := existingExpiry
	if existingExpiry.Before(newExpiry) {
		disposition = "EXTENDED"
		if _, err := tx.Exec(ctx, `
			UPDATE entitlements SET access_ends_at=$1, revision=revision+1, updated_at=$2 WHERE id=$3::uuid
		`, newExpiry, now, entitlementID); err != nil {
			return BundleGrant{}, fmt.Errorf("extending Bundle Course entitlement: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlement_adjustments (
				entitlement_id, old_access_ends_at, new_access_ends_at, reason, actor_account_id, adjusted_at
			) VALUES ($1::uuid, $2, $3, 'Extended by Bundle purchase', $4::uuid, $5)
		`, entitlementID, existingExpiry, newExpiry, adminAccountID, now); err != nil {
			return BundleGrant{}, fmt.Errorf("auditing Bundle entitlement extension: %w", err)
		}
		resulting = newExpiry
	}
	grant := BundleGrant{CourseID: courseID, EntitlementID: entitlementID, Disposition: disposition,
		PreviousAccessEndsAt: &existingExpiry, ResultingAccessEndsAt: resulting}
	if err := insertBundleGrantProvenance(ctx, tx, request, grant, now); err != nil {
		return BundleGrant{}, err
	}
	return grant, nil
}

func insertBundleGrantProvenance(ctx context.Context, tx pgx.Tx, request *PurchaseRequest, grant BundleGrant, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO bundle_purchase_grants (
			purchase_request_id, bundle_id, course_id, entitlement_id, disposition,
			previous_access_ends_at, resulting_access_ends_at, granted_at
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8)
	`, request.ID, *request.BundleID, grant.CourseID, grant.EntitlementID, grant.Disposition,
		grant.PreviousAccessEndsAt, grant.ResultingAccessEndsAt, now)
	if err != nil {
		return fmt.Errorf("recording Bundle grant provenance: %w", err)
	}
	return nil
}

func loadBundleGrants(ctx context.Context, tx pgx.Tx, requestID string) ([]BundleGrant, error) {
	rows, err := tx.Query(ctx, `
		SELECT course_id::text, entitlement_id::text, disposition,
		       previous_access_ends_at, resulting_access_ends_at
		FROM bundle_purchase_grants WHERE purchase_request_id=$1::uuid ORDER BY course_id
	`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading Bundle grants: %w", err)
	}
	defer rows.Close()
	grants := []BundleGrant{}
	for rows.Next() {
		var grant BundleGrant
		if err := rows.Scan(&grant.CourseID, &grant.EntitlementID, &grant.Disposition,
			&grant.PreviousAccessEndsAt, &grant.ResultingAccessEndsAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}
