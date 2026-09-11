package catalogpublic

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BundleMember struct {
	CourseID              string     `json:"course_id"`
	Slug                  string     `json:"slug"`
	Title                 string     `json:"title"`
	InstructorDisplayName string     `json:"instructor_display_name"`
	University            *Taxonomy  `json:"university,omitempty"`
	Major                 *Taxonomy  `json:"major,omitempty"`
	Subject               *Taxonomy  `json:"subject,omitempty"`
	StudyYear             *Taxonomy  `json:"study_year,omitempty"`
	Thumbnail             *Thumbnail `json:"thumbnail,omitempty"`
	Position              int        `json:"position"`
}

type Bundle struct {
	ID          string         `json:"id"`
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	CourseCount int            `json:"course_count"`
	Price       Price          `json:"price"`
	Members     []BundleMember `json:"members"`
}

type BundleListResult struct {
	Items    []Bundle `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int      `json:"total"`
}

func bundleEligibilitySQL(bundleAlias string) string {
	return fmt.Sprintf(`%s.lifecycle='PUBLISHED'
		AND length(trim(%s.description_ar)) > 0
		AND length(trim(%s.description_en)) > 0
		AND EXISTS (SELECT 1 FROM bundle_price_changes bp WHERE bp.bundle_id=%s.id)
		AND (SELECT count(*) FROM bundle_courses members WHERE members.bundle_id=%s.id) >= 2
		AND NOT EXISTS (
			SELECT 1 FROM bundle_courses broken
			JOIN courses member_course ON member_course.id=broken.course_id
			LEFT JOIN course_revisions member_revision ON member_revision.id=member_course.live_revision_id
			WHERE broken.bundle_id=%s.id AND NOT (%s)
		)`, bundleAlias, bundleAlias, bundleAlias, bundleAlias, bundleAlias, bundleAlias, PublishedOnly("member_course", "member_revision"))
}

func (r *Repository) BrowseBundles(ctx context.Context, arabic bool, page, pageSize int) (BundleListResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 12
	}
	eligibility := bundleEligibilitySQL("b")
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM bundles b WHERE `+eligibility).Scan(&total); err != nil {
		return BundleListResult{}, fmt.Errorf("counting public bundles: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		WITH selected AS (
			SELECT b.id, b.slug, b.title_ar, b.title_en, b.description_ar, b.description_en,
			       price.new_value_minor_units, price.offer_price_minor_units,
			       (SELECT count(*) FROM bundle_courses count_members WHERE count_members.bundle_id=b.id)::int AS course_count,
			       b.updated_at
			FROM bundles b
			JOIN LATERAL (
				SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
				WHERE bundle_id=b.id ORDER BY changed_at DESC, id DESC LIMIT 1
			) price ON TRUE
			WHERE `+eligibility+`
			ORDER BY b.updated_at DESC, b.id DESC LIMIT $2 OFFSET $3
		)
		SELECT selected.id::text, selected.slug,
		       CASE WHEN $1 THEN selected.title_ar ELSE selected.title_en END,
		       CASE WHEN $1 THEN selected.description_ar ELSE selected.description_en END,
		       selected.course_count, selected.new_value_minor_units, selected.offer_price_minor_units,
		       bc.course_id::text, c.slug,
		       CASE WHEN $1 THEN cr.title_ar ELSE cr.title_en END, a.display_name, bc.position,
		       CASE WHEN institution.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN institution.name_ar ELSE institution.name_en END END,
		       CASE WHEN major.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN major.label_ar ELSE major.label_en END END,
		       CASE WHEN subject.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN subject.title_ar ELSE subject.title_en END END,
		       subject.official_code, cr.study_year::text,
		       (SELECT mav.id::text FROM media_asset_versions mav
		        JOIN media_assets ma ON ma.id=mav.logical_asset_id
		        JOIN media_thumbnail_variants mtv ON mtv.asset_version_id=mav.id
		        WHERE mav.id=cr.thumbnail_asset_version_id AND mav.kind='THUMBNAIL'
		          AND mav.state='READY' AND ma.kind='THUMBNAIL' AND ma.course_id=c.id
		          AND ma.retired_at IS NULL LIMIT 1)
		FROM selected
		JOIN bundle_courses bc ON bc.bundle_id=selected.id
		JOIN courses c ON c.id=bc.course_id
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		JOIN accounts a ON a.id=c.owner_account_id
		LEFT JOIN institutions institution ON institution.id=c.institution_id
		LEFT JOIN taxonomy_terms major ON major.id=cr.major_term_id
		LEFT JOIN subjects subject ON subject.id=c.subject_id
		ORDER BY selected.updated_at DESC, selected.id DESC, bc.position, bc.course_id
	`, arabic, pageSize, (page-1)*pageSize)
	if err != nil {
		return BundleListResult{}, fmt.Errorf("browsing public bundles: %w", err)
	}
	defer rows.Close()
	items, err := scanBundles(rows, arabic, true)
	if err != nil {
		return BundleListResult{}, err
	}
	return BundleListResult{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (r *Repository) BundleDetail(ctx context.Context, identifier string, arabic bool) (*Bundle, error) {
	predicate := "AND b.slug=$2"
	if parsed, err := uuid.Parse(identifier); err == nil && parsed.String() == identifier {
		predicate = "AND b.id=$2::uuid"
	}
	rows, err := r.pool.Query(ctx, `
		SELECT b.id::text, b.slug, CASE WHEN $1 THEN b.title_ar ELSE b.title_en END,
		       CASE WHEN $1 THEN b.description_ar ELSE b.description_en END,
		       (SELECT count(*) FROM bundle_courses count_members WHERE count_members.bundle_id=b.id)::int,
		       price.new_value_minor_units, price.offer_price_minor_units,
		       bc.course_id::text, c.slug, CASE WHEN $1 THEN cr.title_ar ELSE cr.title_en END,
		       a.display_name, bc.position,
		       CASE WHEN institution.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN institution.name_ar ELSE institution.name_en END END,
		       CASE WHEN major.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN major.label_ar ELSE major.label_en END END,
		       CASE WHEN subject.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN subject.title_ar ELSE subject.title_en END END,
		       subject.official_code, cr.study_year::text,
		       (SELECT mav.id::text FROM media_asset_versions mav
		        JOIN media_assets ma ON ma.id=mav.logical_asset_id
		        JOIN media_thumbnail_variants mtv ON mtv.asset_version_id=mav.id
		        WHERE mav.id=cr.thumbnail_asset_version_id AND mav.kind='THUMBNAIL'
		          AND mav.state='READY' AND ma.kind='THUMBNAIL' AND ma.course_id=c.id
		          AND ma.retired_at IS NULL LIMIT 1)
		FROM bundles b
		JOIN LATERAL (
			SELECT new_value_minor_units, offer_price_minor_units FROM bundle_price_changes
			WHERE bundle_id=b.id ORDER BY changed_at DESC, id DESC LIMIT 1
		) price ON TRUE
		JOIN bundle_courses bc ON bc.bundle_id=b.id
		JOIN courses c ON c.id=bc.course_id
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		JOIN accounts a ON a.id=c.owner_account_id
		LEFT JOIN institutions institution ON institution.id=c.institution_id
		LEFT JOIN taxonomy_terms major ON major.id=cr.major_term_id
		LEFT JOIN subjects subject ON subject.id=c.subject_id
		WHERE `+bundleEligibilitySQL("b")+` `+predicate+`
		ORDER BY bc.position, bc.course_id
	`, arabic, identifier)
	if err != nil {
		return nil, fmt.Errorf("loading public bundle: %w", err)
	}
	defer rows.Close()
	items, err := scanBundles(rows, arabic, false)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

func scanBundles(rows pgx.Rows, arabic, previewsOnly bool) ([]Bundle, error) {
	items := []Bundle{}
	index := map[string]int{}
	for rows.Next() {
		var id, slug, title, description string
		var courseCount, position int
		var regular int64
		var offer *int64
		var courseID, courseSlug, courseTitle, instructor string
		var university, major, subject, subjectCode, studyYear, thumbnailID *string
		if err := rows.Scan(&id, &slug, &title, &description, &courseCount, &regular, &offer,
			&courseID, &courseSlug, &courseTitle, &instructor, &position,
			&university, &major, &subject, &subjectCode, &studyYear, &thumbnailID); err != nil {
			return nil, fmt.Errorf("scanning public bundle: %w", err)
		}
		at, exists := index[id]
		if !exists {
			effective := regular
			if offer != nil {
				effective = *offer
			}
			items = append(items, Bundle{ID: id, Slug: slug, Title: title, Description: description,
				CourseCount: courseCount, Members: []BundleMember{},
				Price: Price{MinorUnits: effective, RegularMinorUnits: regular, OfferMinorUnits: offer, Currency: currencyKWD}})
			at = len(items) - 1
			index[id] = at
		}
		if previewsOnly && len(items[at].Members) >= 3 {
			continue
		}
		member := BundleMember{CourseID: courseID, Slug: courseSlug, Title: courseTitle,
			InstructorDisplayName: instructor, Position: position}
		if university != nil {
			member.University = &Taxonomy{Label: *university}
		}
		if major != nil {
			member.Major = &Taxonomy{Label: *major}
		}
		if subject != nil {
			member.Subject = &Taxonomy{Label: *subject, Code: subjectCode}
		}
		if studyYear != nil {
			if label, ok := localizedStudyYear(*studyYear, arabic); ok {
				member.StudyYear = &Taxonomy{Label: label}
			}
		}
		if thumbnailID != nil {
			prefix := "/api/v1/catalog/courses/" + courseID + "/thumbnails/" + *thumbnailID
			member.Thumbnail = &Thumbnail{AssetVersionID: *thumbnailID, CardURL: prefix + "/card", LargeURL: prefix + "/large"}
		}
		items[at].Members = append(items[at].Members, member)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("reading public bundles: %w", err)
	}
	return items, nil
}
