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
)

type BundleMember struct {
	CourseID              string `json:"course_id"`
	Position              int    `json:"position"`
	TitleAr               string `json:"title_ar"`
	TitleEn               string `json:"title_en"`
	InstructorDisplayName string `json:"instructor_display_name"`
}

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
	Eligible      bool            `json:"eligible"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
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
		bundle.Members, err = loadBundleMembersTx(ctx, tx, bundle.ID)
		if err != nil {
			return err
		}
		bundle.Eligible = bundlePublicationReady(bundle.DescriptionAr, bundle.DescriptionEn, len(bundle.Members), bundle.Price != nil)
		bundle.CourseCount = len(bundle.Members)
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
		bundle.Members, err = loadBundleMembersTx(ctx, tx, bundle.ID)
		if err != nil {
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
		bundle.CourseCount = len(bundle.Members)
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
		SELECT bc.course_id::text, bc.position, cr.title_ar, cr.title_en, a.display_name
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
		if err := rows.Scan(&member.CourseID, &member.Position, &member.TitleAr, &member.TitleEn, &member.InstructorDisplayName); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
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
	bundle.Members, err = loadBundleMembersTx(ctx, tx, bundle.ID)
	if err != nil {
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
	bundle.CourseCount = len(bundle.Members)
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
		       ) AS eligible
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
			&regular, &offer, &count, &item.Eligible); err != nil {
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
	return items, rows.Err()
}
