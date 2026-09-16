package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

type BundleLifecycle string

const (
	BundleDraft     BundleLifecycle = "DRAFT"
	BundlePublished BundleLifecycle = "PUBLISHED"
	BundleDelisted  BundleLifecycle = "DELISTED"
	BundleArchived  BundleLifecycle = "ARCHIVED"
)

var (
	ErrBundleNotFound        = errors.New("bundle not found")
	ErrBundleMemberCount     = errors.New("bundle requires at least two distinct courses")
	ErrBundleMemberInvalid   = errors.New("bundle members must be publicly eligible courses")
	ErrBundleVersionConflict = errors.New("bundle revision conflict")
	ErrBundleLifecycle       = errors.New("invalid bundle lifecycle transition")
	ErrBundlePriceRequired   = errors.New("bundle price is required")
	ErrBundleDescription     = errors.New("Arabic and English bundle descriptions are required")
	// ErrBundleReferenced reports a Bundle that commerce history already points
	// at. Hard deletion would orphan a purchase snapshot a Student and an Admin
	// both rely on, so the Bundle stays and the caller is told to use the
	// lifecycle instead.
	ErrBundleReferenced = errors.New("bundle is referenced by purchase history")
)

type BundleMember struct {
	CourseID              string `json:"course_id"`
	Position              int    `json:"position"`
	TitleAr               string `json:"title_ar"`
	TitleEn               string `json:"title_en"`
	InstructorDisplayName string `json:"instructor_display_name"`
	// EffectiveMinorUnits is what this Course sells for on its own today, so
	// the Admin can see what the Bundle is discounting against. Absent when
	// the Course has never been priced.
	EffectiveMinorUnits *int64 `json:"effective_minor_units,omitempty"`
}

// sumMemberPrices totals the standalone price of every member. It returns nil
// unless every member is priced, because a sum missing a member is not a
// smaller total -- it is a wrong one.
func sumMemberPrices(members []BundleMember) *int64 {
	if len(members) == 0 {
		return nil
	}
	total := int64(0)
	for _, member := range members {
		if member.EffectiveMinorUnits == nil {
			return nil
		}
		total += *member.EffectiveMinorUnits
	}
	return &total
}

// memberPriceSelect is the standalone effective price of one Course: the offer
// when one is live, otherwise the regular price. Course-level only --
// section_id IS NULL -- matching how the Course catalogue itself prices.
const memberPriceSelect = `(
	SELECT COALESCE(cpc.offer_price_minor_units, cpc.new_value_minor_units)
	FROM course_price_changes cpc
	WHERE cpc.course_id = %s AND cpc.section_id IS NULL
	ORDER BY cpc.changed_at DESC, cpc.id DESC LIMIT 1
)`

type Bundle struct {
	ID            string          `json:"id"`
	Slug          string          `json:"slug"`
	Lifecycle     BundleLifecycle `json:"lifecycle"`
	TitleAr       string          `json:"title_ar"`
	TitleEn       string          `json:"title_en"`
	DescriptionAr string          `json:"description_ar"`
	DescriptionEn string          `json:"description_en"`
	Revision      int64           `json:"revision"`
	Price         *CatalogPrice   `json:"price,omitempty"`
	Members       []BundleMember  `json:"members"`
	CourseCount   int             `json:"course_count"`
	// MemberTotalMinorUnits is the summed effective price of every member
	// Course, and is present only when every member actually carries a price.
	// A partial sum would understate the total and turn the Admin savings line
	// into a false claim, so the field is omitted instead of guessed.
	MemberTotalMinorUnits *int64 `json:"member_total_minor_units,omitempty"`
	// Deletable reports whether hard deletion is available right now: no
	// purchase request has ever referenced this Bundle. The frontend renders
	// the action from this field; the backend enforces it regardless.
	Deletable bool      `json:"deletable"`
	Eligible  bool      `json:"eligible"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BundlePriceInput struct {
	RegularMinorUnits int64
	OfferMinorUnits   *int64
	Reason            string
}

type CreateBundleRequest struct {
	TitleAr, TitleEn             string
	DescriptionAr, DescriptionEn string
	CourseIDs                    []string
	Price                        *BundlePriceInput
	AdminAccountID               string
	ActorDescriptor              string
}

type UpdateBundleRequest struct {
	BundleID                     string
	ExpectedRevision             int64
	TitleAr, TitleEn             string
	DescriptionAr, DescriptionEn string
	CourseIDs                    []string
	Price                        *BundlePriceInput
	AdminAccountID               string
	ActorDescriptor              string
}

type TransitionBundleRequest struct {
	BundleID         string
	ExpectedRevision int64
	Target           BundleLifecycle
	AdminAccountID   string
	ActorDescriptor  string
}

func validateBundleFields(titleAr, titleEn string, courseIDs []string) error {
	if strings.TrimSpace(titleAr) == "" || strings.TrimSpace(titleEn) == "" {
		return errors.New("Arabic and English bundle titles are required")
	}
	seen := make(map[string]struct{}, len(courseIDs))
	for _, id := range courseIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(id))
		if err != nil {
			return ErrBundleMemberInvalid
		}
		key := parsed.String()
		if _, exists := seen[key]; exists {
			return ErrBundleMemberCount
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validatePublishedBundle(titleAr, titleEn, descriptionAr, descriptionEn string, courseIDs []string) error {
	if err := validateBundleFields(titleAr, titleEn, courseIDs); err != nil {
		return err
	}
	if strings.TrimSpace(descriptionAr) == "" || strings.TrimSpace(descriptionEn) == "" {
		return ErrBundleDescription
	}
	if len(courseIDs) < 2 {
		return ErrBundleMemberCount
	}
	return nil
}

func bundlePublicationReady(descriptionAr, descriptionEn string, courseCount int, priced bool) bool {
	return priced && courseCount >= 2 && strings.TrimSpace(descriptionAr) != "" && strings.TrimSpace(descriptionEn) != ""
}

func (r *Repository) CreateBundle(ctx context.Context, req CreateBundleRequest) (*Bundle, error) {
	if err := validateBundleFields(req.TitleAr, req.TitleEn, req.CourseIDs); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(req.AdminAccountID); err != nil {
		return nil, errors.New("admin account ID is required")
	}
	if req.Price != nil {
		if _, err := NewCatalogPrice(req.Price.RegularMinorUnits, req.Price.OfferMinorUnits); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Price.Reason) == "" {
			return nil, ErrReasonRequired
		}
	}
	var result *Bundle
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		if err := lockEligibleBundleCourses(ctx, tx, req.CourseIDs); err != nil {
			return err
		}
		var bundle Bundle
		err := tx.QueryRow(ctx, `
			INSERT INTO bundles (
				title_ar, title_en, description_ar, description_en,
				created_by_account_id, updated_by_account_id
			) VALUES ($1, $2, $3, $4, $5::uuid, $5::uuid)
			RETURNING id::text, slug, lifecycle, title_ar, title_en, description_ar,
			          description_en, revision, created_at, updated_at
		`, strings.TrimSpace(req.TitleAr), strings.TrimSpace(req.TitleEn),
			req.DescriptionAr, req.DescriptionEn, req.AdminAccountID).Scan(
			&bundle.ID, &bundle.Slug, &bundle.Lifecycle, &bundle.TitleAr, &bundle.TitleEn,
			&bundle.DescriptionAr, &bundle.DescriptionEn, &bundle.Revision,
			&bundle.CreatedAt, &bundle.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("creating bundle: %w", err)
		}
		if err := replaceBundleMembers(ctx, tx, bundle.ID, req.CourseIDs); err != nil {
			return err
		}
		if req.Price != nil {
			price, err := appendBundlePrice(ctx, tx, bundle.ID, req.AdminAccountID, *req.Price)
			if err != nil {
				return err
			}
			bundle.Price = price
		}
		actor := req.AdminAccountID
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID: &actor, ActorRole: "ADMIN", ActorDescriptor: req.ActorDescriptor,
			Action: "BUNDLE_CREATED", TargetType: "BUNDLE", TargetID: bundle.ID,
			Reason: "Bundle draft created by Admin", Metadata: map[string]any{"course_ids": req.CourseIDs},
		}); err != nil {
			return err
		}
		if err := hydrateBundleTx(ctx, tx, &bundle); err != nil {
			return err
		}
		bundle.Eligible = bundlePublicationReady(bundle.DescriptionAr, bundle.DescriptionEn, len(bundle.Members), bundle.Price != nil)
		result = &bundle
		return nil
	})
	return result, err
}

func (r *Repository) UpdateBundle(ctx context.Context, req UpdateBundleRequest) (*Bundle, error) {
	if _, err := uuid.Parse(req.BundleID); err != nil {
		return nil, ErrBundleNotFound
	}
	if err := validateBundleFields(req.TitleAr, req.TitleEn, req.CourseIDs); err != nil {
		return nil, err
	}
	if req.Price != nil {
		if _, err := NewCatalogPrice(req.Price.RegularMinorUnits, req.Price.OfferMinorUnits); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Price.Reason) == "" {
			return nil, ErrReasonRequired
		}
	}
	var result *Bundle
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		bundle, err := lockBundle(ctx, tx, req.BundleID)
		if err != nil {
			return err
		}
		if bundle.Lifecycle == BundleArchived {
			return ErrBundleLifecycle
		}
		if req.ExpectedRevision < 1 || bundle.Revision != req.ExpectedRevision {
			return ErrBundleVersionConflict
		}
		if bundle.Lifecycle != BundleDraft {
			if err := validatePublishedBundle(req.TitleAr, req.TitleEn, req.DescriptionAr, req.DescriptionEn, req.CourseIDs); err != nil {
				return err
			}
		}
		if err := lockEligibleBundleCourses(ctx, tx, req.CourseIDs); err != nil {
			return err
		}
		if err := replaceBundleMembers(ctx, tx, bundle.ID, req.CourseIDs); err != nil {
			return err
		}
		if req.Price != nil {
			price, err := appendBundlePrice(ctx, tx, bundle.ID, req.AdminAccountID, *req.Price)
			if err != nil {
				return err
			}
			bundle.Price = price
		}
		bundle.Price, err = loadBundlePriceTx(ctx, tx, bundle.ID)
		if err != nil {
			return err
		}
		if bundle.Lifecycle != BundleDraft && bundle.Price == nil {
			return ErrBundlePriceRequired
		}
		now := time.Now().UTC()
		err = tx.QueryRow(ctx, `
			UPDATE bundles SET title_ar=$1, title_en=$2, description_ar=$3, description_en=$4,
			       revision=revision+1, updated_by_account_id=$5::uuid, updated_at=$6
			 WHERE id=$7::uuid
			 RETURNING lifecycle, revision, created_at, updated_at, slug
		`, strings.TrimSpace(req.TitleAr), strings.TrimSpace(req.TitleEn), req.DescriptionAr,
			req.DescriptionEn, req.AdminAccountID, now, req.BundleID).Scan(
			&bundle.Lifecycle, &bundle.Revision, &bundle.CreatedAt, &bundle.UpdatedAt, &bundle.Slug,
		)
		if err != nil {
			return fmt.Errorf("updating bundle: %w", err)
		}
		bundle.TitleAr, bundle.TitleEn = strings.TrimSpace(req.TitleAr), strings.TrimSpace(req.TitleEn)
		bundle.DescriptionAr, bundle.DescriptionEn = req.DescriptionAr, req.DescriptionEn
		if err := hydrateBundleTx(ctx, tx, bundle); err != nil {
			return err
		}
		actor := req.AdminAccountID
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID: &actor, ActorRole: "ADMIN", ActorDescriptor: req.ActorDescriptor,
			Action: "BUNDLE_UPDATED", TargetType: "BUNDLE", TargetID: bundle.ID,
			Reason:   "Bundle metadata and membership updated by Admin",
			Metadata: map[string]any{"course_ids": req.CourseIDs, "revision": bundle.Revision},
		}); err != nil {
			return err
		}
		bundle.Eligible = bundlePublicationReady(bundle.DescriptionAr, bundle.DescriptionEn, len(bundle.Members), bundle.Price != nil)
		result = bundle
		return nil
	})
	return result, err
}

func (r *Repository) TransitionBundle(ctx context.Context, req TransitionBundleRequest) (*Bundle, error) {
	var result *Bundle
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		bundle, err := lockBundle(ctx, tx, req.BundleID)
		if err != nil {
			return err
		}
		if req.ExpectedRevision < 1 || bundle.Revision != req.ExpectedRevision {
			return ErrBundleVersionConflict
		}
		if !bundleTransitionAllowed(bundle.Lifecycle, req.Target) {
			return ErrBundleLifecycle
		}
		if req.Target == BundlePublished {
			members, err := loadBundleCourseIDsTx(ctx, tx, bundle.ID)
			if err != nil {
				return err
			}
			if err := validatePublishedBundle(bundle.TitleAr, bundle.TitleEn, bundle.DescriptionAr, bundle.DescriptionEn, members); err != nil {
				return err
			}
			if err := lockEligibleBundleCourses(ctx, tx, members); err != nil {
				return err
			}
			price, err := loadBundlePriceTx(ctx, tx, bundle.ID)
			if err != nil {
				return err
			}
			if price == nil {
				return ErrBundlePriceRequired
			}
			bundle.Price = price
		}
		now := time.Now().UTC()
		if err := tx.QueryRow(ctx, `
			UPDATE bundles SET lifecycle=$1, revision=revision+1,
			       updated_by_account_id=$2::uuid, updated_at=$3 WHERE id=$4::uuid
			RETURNING revision, updated_at
		`, req.Target, req.AdminAccountID, now, bundle.ID).Scan(&bundle.Revision, &bundle.UpdatedAt); err != nil {
			return fmt.Errorf("transitioning bundle: %w", err)
		}
		bundle.Lifecycle = req.Target
		bundle.Eligible = req.Target == BundlePublished
		if bundle.Price == nil {
			if bundle.Price, err = loadBundlePriceTx(ctx, tx, bundle.ID); err != nil {
				return err
			}
		}
		if err := hydrateBundleTx(ctx, tx, bundle); err != nil {
			return err
		}
		actor := req.AdminAccountID
		action := map[BundleLifecycle]string{
			BundlePublished: "BUNDLE_PUBLISHED", BundleDelisted: "BUNDLE_DELISTED", BundleArchived: "BUNDLE_ARCHIVED",
		}[req.Target]
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID: &actor, ActorRole: "ADMIN", ActorDescriptor: req.ActorDescriptor,
			Action: action, TargetType: "BUNDLE", TargetID: bundle.ID,
			Reason: "Bundle lifecycle changed by Admin", Metadata: map[string]any{"to": req.Target, "revision": bundle.Revision},
		}); err != nil {
			return err
		}
		result = bundle
		return nil
	})
	return result, err
}

func bundleTransitionAllowed(from, to BundleLifecycle) bool {
	switch from {
	case BundleDraft:
		return to == BundlePublished || to == BundleArchived
	case BundlePublished:
		return to == BundleDelisted || to == BundleArchived
	case BundleDelisted:
		return to == BundlePublished || to == BundleArchived
	default:
		return false
	}
}

func lockBundle(ctx context.Context, tx pgx.Tx, id string) (*Bundle, error) {
	var bundle Bundle
	err := tx.QueryRow(ctx, `
		SELECT id::text, slug, lifecycle, title_ar, title_en, description_ar, description_en,
		       revision, created_at, updated_at FROM bundles WHERE id=$1::uuid FOR UPDATE
	`, id).Scan(&bundle.ID, &bundle.Slug, &bundle.Lifecycle, &bundle.TitleAr, &bundle.TitleEn,
		&bundle.DescriptionAr, &bundle.DescriptionEn, &bundle.Revision, &bundle.CreatedAt, &bundle.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBundleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking bundle: %w", err)
	}
	return &bundle, nil
}

func lockEligibleBundleCourses(ctx context.Context, tx pgx.Tx, courseIDs []string) error {
	ids := append([]string(nil), courseIDs...)
	sort.Strings(ids)
	rows, err := tx.Query(ctx, `
		SELECT c.id::text FROM courses c
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		WHERE c.id = ANY($1::uuid[]) AND `+catalogpublic.PublishedOnly("c", "cr")+`
		ORDER BY c.id FOR SHARE OF c, cr
	`, ids)
	if err != nil {
		return fmt.Errorf("locking bundle courses: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading bundle courses: %w", err)
	}
	if count != len(ids) {
		return ErrBundleMemberInvalid
	}
	return nil
}

func replaceBundleMembers(ctx context.Context, tx pgx.Tx, bundleID string, courseIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM bundle_courses WHERE bundle_id=$1::uuid`, bundleID); err != nil {
		return fmt.Errorf("replacing bundle members: %w", err)
	}
	for position, courseID := range courseIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO bundle_courses (bundle_id, course_id, position) VALUES ($1::uuid, $2::uuid, $3)
		`, bundleID, courseID, position); err != nil {
			return fmt.Errorf("inserting bundle member: %w", err)
		}
	}
	return nil
}

func appendBundlePrice(ctx context.Context, tx pgx.Tx, bundleID, adminID string, input BundlePriceInput) (*CatalogPrice, error) {
	price, err := NewCatalogPrice(input.RegularMinorUnits, input.OfferMinorUnits)
	if err != nil {
		return nil, err
	}
	var oldRegular, oldOffer *int64
	err = tx.QueryRow(ctx, `
		SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
		WHERE bundle_id=$1::uuid ORDER BY changed_at DESC, id DESC LIMIT 1
	`, bundleID).Scan(&oldRegular, &oldOffer)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("loading bundle price: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO bundle_price_changes (
			bundle_id, old_value_minor_units, new_value_minor_units,
			old_offer_price_minor_units, offer_price_minor_units,
			changed_by_account_id, reason
		) VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7)
	`, bundleID, oldRegular, input.RegularMinorUnits, oldOffer, input.OfferMinorUnits, adminID, strings.TrimSpace(input.Reason))
	if err != nil {
		return nil, fmt.Errorf("appending bundle price: %w", err)
	}
	actor := adminID
	if err := WriteAuditEvent(ctx, tx, AuditEvent{
		ActorAccountID: &actor, ActorRole: "ADMIN", ActorDescriptor: adminID,
		Action: "BUNDLE_PRICE_CHANGED", TargetType: "BUNDLE", TargetID: bundleID,
		Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{
			"old_regular_minor_units": oldRegular, "regular_minor_units": input.RegularMinorUnits,
			"old_offer_minor_units": oldOffer, "offer_minor_units": input.OfferMinorUnits,
		},
	}); err != nil {
		return nil, err
	}
	return &price, nil
}

func loadBundlePriceTx(ctx context.Context, tx pgx.Tx, bundleID string) (*CatalogPrice, error) {
	var regular int64
	var offer *int64
	err := tx.QueryRow(ctx, `
		SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
		WHERE bundle_id=$1::uuid ORDER BY changed_at DESC, id DESC LIMIT 1
	`, bundleID).Scan(&regular, &offer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading bundle price: %w", err)
	}
	price, err := NewCatalogPrice(regular, offer)
	return &price, err
}

func loadBundleCourseIDsTx(ctx context.Context, tx pgx.Tx, bundleID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT course_id::text FROM bundle_courses WHERE bundle_id=$1::uuid ORDER BY position, course_id`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("loading bundle course IDs: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func loadBundleMembersTx(ctx context.Context, tx pgx.Tx, bundleID string) ([]BundleMember, error) {
	rows, err := tx.Query(ctx, `
		SELECT bc.course_id::text, bc.position, cr.title_ar, cr.title_en, a.display_name,
		       `+fmt.Sprintf(memberPriceSelect, "c.id")+`
		FROM bundle_courses bc
		JOIN courses c ON c.id=bc.course_id
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		JOIN accounts a ON a.id=c.owner_account_id
		WHERE bc.bundle_id=$1::uuid ORDER BY bc.position, bc.course_id
	`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("loading bundle members: %w", err)
	}
	defer rows.Close()
	members := []BundleMember{}
	for rows.Next() {
		var member BundleMember
		if err := rows.Scan(&member.CourseID, &member.Position, &member.TitleAr, &member.TitleEn,
			&member.InstructorDisplayName, &member.EffectiveMinorUnits); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// bundleReferencedTx reports whether commerce history points at this Bundle.
// purchase_requests is the single entry point: bundle_purchase_grants and
// purchase_request_bundle_items both hang off a purchase request, so a Bundle
// with no purchase request has no historical record to preserve.
func bundleReferencedTx(ctx context.Context, tx pgx.Tx, bundleID string) (bool, error) {
	var referenced bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM purchase_requests WHERE bundle_id=$1::uuid)
	`, bundleID).Scan(&referenced); err != nil {
		return false, fmt.Errorf("checking bundle references: %w", err)
	}
	return referenced, nil
}

// hydrateBundleTx fills the presentation-only fields every Admin response
// carries, so no mutation reply can come back with a truthful lifecycle and an
// empty membership beside it.
func hydrateBundleTx(ctx context.Context, tx pgx.Tx, bundle *Bundle) error {
	members, err := loadBundleMembersTx(ctx, tx, bundle.ID)
	if err != nil {
		return err
	}
	bundle.Members = members
	bundle.CourseCount = len(members)
	bundle.MemberTotalMinorUnits = sumMemberPrices(members)
	referenced, err := bundleReferencedTx(ctx, tx, bundle.ID)
	if err != nil {
		return err
	}
	bundle.Deletable = !referenced
	return nil
}

// DeleteBundleRequest asks for irreversible removal of one Bundle at one
// revision. The revision is required for the same reason every other Bundle
// mutation requires it: an Admin must not delete the row a second tab just
// changed underneath them.
type DeleteBundleRequest struct {
	BundleID         string
	ExpectedRevision int64
	AdminAccountID   string
	ActorDescriptor  string
}

// DeleteBundle hard-deletes a Bundle that no purchase request has ever
// referenced.
//
// Deletion is deliberately narrow. A Bundle that has been purchased -- or even
// only requested -- carries a commercial snapshot that Students, Admins and
// the audit record all read, so it is refused here rather than cascaded away;
// ARCHIVED remains the supported end state for those. Everything the deleted
// Bundle owned outright (its membership rows and its price history) goes with
// it, and the audit event is written first so the deletion itself survives the
// row it describes.
func (r *Repository) DeleteBundle(ctx context.Context, req DeleteBundleRequest) error {
	if _, err := uuid.Parse(req.BundleID); err != nil {
		return ErrBundleNotFound
	}
	if _, err := uuid.Parse(req.AdminAccountID); err != nil {
		return errors.New("admin account ID is required")
	}
	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		bundle, err := lockBundle(ctx, tx, req.BundleID)
		if err != nil {
			return err
		}
		if req.ExpectedRevision < 1 || bundle.Revision != req.ExpectedRevision {
			return ErrBundleVersionConflict
		}
		referenced, err := bundleReferencedTx(ctx, tx, bundle.ID)
		if err != nil {
			return err
		}
		if referenced {
			return ErrBundleReferenced
		}
		actor := req.AdminAccountID
		if err := WriteAuditEvent(ctx, tx, AuditEvent{
			ActorAccountID: &actor, ActorRole: "ADMIN", ActorDescriptor: req.ActorDescriptor,
			Action: "BUNDLE_DELETED", TargetType: "BUNDLE", TargetID: bundle.ID,
			Reason: "Unreferenced Bundle deleted by Admin",
			Metadata: map[string]any{
				"lifecycle": bundle.Lifecycle, "revision": bundle.Revision,
				"title_en": bundle.TitleEn, "title_ar": bundle.TitleAr, "slug": bundle.Slug,
			},
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM bundle_price_changes WHERE bundle_id=$1::uuid`, bundle.ID); err != nil {
			return fmt.Errorf("deleting bundle price history: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM bundle_courses WHERE bundle_id=$1::uuid`, bundle.ID); err != nil {
			return fmt.Errorf("deleting bundle members: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM bundles WHERE id=$1::uuid`, bundle.ID)
		if err != nil {
			return fmt.Errorf("deleting bundle: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrBundleNotFound
		}
		return nil
	})
}

func (r *Repository) GetBundle(ctx context.Context, bundleID string) (*Bundle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	bundle, err := lockBundle(ctx, tx, bundleID)
	if err != nil {
		return nil, err
	}
	bundle.Price, err = loadBundlePriceTx(ctx, tx, bundle.ID)
	if err != nil {
		return nil, err
	}
	if err := hydrateBundleTx(ctx, tx, bundle); err != nil {
		return nil, err
	}
	var memberEligible bool
	if err := tx.QueryRow(ctx, `
		SELECT count(bc.course_id) >= 2 AND bool_and(
			c.lifecycle='PUBLISHED' AND c.access_suspended_at IS NULL
			AND c.retired_at IS NULL AND c.live_revision_id=cr.id
		)
		FROM bundle_courses bc
		JOIN courses c ON c.id=bc.course_id
		LEFT JOIN course_revisions cr ON cr.id=c.live_revision_id
		WHERE bc.bundle_id=$1::uuid
	`, bundle.ID).Scan(&memberEligible); err != nil {
		return nil, fmt.Errorf("checking Bundle eligibility: %w", err)
	}
	bundle.Eligible = bundle.Price != nil && memberEligible && bundlePublicationReady(bundle.DescriptionAr, bundle.DescriptionEn, len(bundle.Members), true)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return bundle, nil
}

func (r *Repository) ListBundles(ctx context.Context) ([]Bundle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.id::text, b.slug, b.lifecycle, b.title_ar, b.title_en, b.description_ar,
		       b.description_en, b.revision, b.created_at, b.updated_at,
		       price.new_value_minor_units, price.offer_price_minor_units,
		       count(bc.course_id),
			price.new_value_minor_units IS NOT NULL
			AND length(trim(b.description_ar)) > 0
			AND length(trim(b.description_en)) > 0
			AND count(bc.course_id) >= 2 AND bool_and(
		           c.lifecycle='PUBLISHED' AND c.access_suspended_at IS NULL
		           AND c.retired_at IS NULL AND c.live_revision_id=cr.id
		       ) AS eligible,
		       NOT EXISTS (SELECT 1 FROM purchase_requests pr WHERE pr.bundle_id=b.id) AS deletable
		FROM bundles b
		LEFT JOIN bundle_courses bc ON bc.bundle_id=b.id
		LEFT JOIN courses c ON c.id=bc.course_id
		LEFT JOIN course_revisions cr ON cr.id=c.live_revision_id
		LEFT JOIN LATERAL (
			SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
			WHERE bundle_id=b.id ORDER BY changed_at DESC, id DESC LIMIT 1
		) price ON TRUE
		GROUP BY b.id, price.new_value_minor_units, price.offer_price_minor_units
		ORDER BY b.updated_at DESC, b.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing bundles: %w", err)
	}
	defer rows.Close()
	items := []Bundle{}
	for rows.Next() {
		var item Bundle
		var regular, offer *int64
		var count int
		if err := rows.Scan(&item.ID, &item.Slug, &item.Lifecycle, &item.TitleAr, &item.TitleEn,
			&item.DescriptionAr, &item.DescriptionEn, &item.Revision, &item.CreatedAt, &item.UpdatedAt,
			&regular, &offer, &count, &item.Eligible, &item.Deletable); err != nil {
			return nil, err
		}
		if regular != nil {
			price, err := NewCatalogPrice(*regular, offer)
			if err != nil {
				return nil, err
			}
			item.Price = &price
		}
		item.Members = make([]BundleMember, 0)
		item.CourseCount = count
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Membership is loaded for the list, not only for the detail read. The
	// Admin list is where a Bundle is recognised, and "3 Courses" with no names
	// beside it is not enough to tell two draft Bundles apart -- which is
	// exactly the state that makes a delete feel unsafe. One extra query keyed
	// by the ids already selected, rather than N detail reads.
	if err := r.attachBundleMembers(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) attachBundleMembers(ctx context.Context, items []Bundle) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	index := make(map[string]int, len(items))
	for position, item := range items {
		ids = append(ids, item.ID)
		index[item.ID] = position
	}
	rows, err := r.pool.Query(ctx, `
		SELECT bc.bundle_id::text, bc.course_id::text, bc.position, cr.title_ar, cr.title_en,
		       a.display_name, `+fmt.Sprintf(memberPriceSelect, "c.id")+`
		FROM bundle_courses bc
		JOIN courses c ON c.id=bc.course_id
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		JOIN accounts a ON a.id=c.owner_account_id
		WHERE bc.bundle_id = ANY($1::uuid[])
		ORDER BY bc.bundle_id, bc.position, bc.course_id
	`, ids)
	if err != nil {
		return fmt.Errorf("listing bundle members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bundleID string
		var member BundleMember
		if err := rows.Scan(&bundleID, &member.CourseID, &member.Position, &member.TitleAr,
			&member.TitleEn, &member.InstructorDisplayName, &member.EffectiveMinorUnits); err != nil {
			return err
		}
		if position, ok := index[bundleID]; ok {
			items[position].Members = append(items[position].Members, member)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for position := range items {
		items[position].MemberTotalMinorUnits = sumMemberPrices(items[position].Members)
	}
	return nil
}
