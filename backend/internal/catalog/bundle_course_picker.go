package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

const BundleCoursePickerPageSize = 20

type BundleCourseOption struct {
	ID                    string        `json:"id"`
	TitleAr               string        `json:"title_ar"`
	TitleEn               string        `json:"title_en"`
	InstructorDisplayName string        `json:"instructor_display_name"`
	Price                 *CatalogPrice `json:"price,omitempty"`
}

type BundleCoursePage struct {
	Items    []BundleCourseOption `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int                  `json:"total"`
	HasNext  bool                 `json:"has_next"`
}

func bundleCoursePickerWhere() string {
	return catalogpublic.PublishedOnly("c", "cr") + `
		AND ($1 = '' OR cr.title_ar ILIKE '%' || $1 || '%' OR cr.title_en ILIKE '%' || $1 || '%'
		     OR c.id::text ILIKE '%' || $1 || '%')`
}

func (r *Repository) countEligibleBundleCourses(ctx context.Context, where, search string) (int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM courses c
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		WHERE `+where, search).Scan(&total); err != nil {
		return 0, fmt.Errorf("counting eligible Bundle Courses: %w", err)
	}
	return total, nil
}

func scanBundleCourseOptions(rows pgx.Rows) ([]BundleCourseOption, error) {
	items := make([]BundleCourseOption, 0, BundleCoursePickerPageSize)
	for rows.Next() {
		var option BundleCourseOption
		var regular, offer *int64
		if err := rows.Scan(&option.ID, &option.TitleAr, &option.TitleEn, &option.InstructorDisplayName, &regular, &offer); err != nil {
			return nil, fmt.Errorf("scanning eligible Bundle Course: %w", err)
		}
		if regular != nil {
			price, err := NewCatalogPrice(*regular, offer)
			if err != nil {
				return nil, err
			}
			option.Price = &price
		}
		items = append(items, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading eligible Bundle Courses: %w", err)
	}
	return items, nil
}

func (r *Repository) ListEligibleBundleCourses(ctx context.Context, page int, search string) (BundleCoursePage, error) {
	if page < 1 {
		page = 1
	}
	query := strings.TrimSpace(search)
	where := bundleCoursePickerWhere()
	total, err := r.countEligibleBundleCourses(ctx, where, query)
	if err != nil {
		return BundleCoursePage{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, cr.title_ar, cr.title_en, a.display_name,
		       price.new_value_minor_units, price.offer_price_minor_units
		FROM courses c
		JOIN course_revisions cr ON cr.id=c.live_revision_id
		JOIN accounts a ON a.id=c.owner_account_id
		LEFT JOIN LATERAL (
			SELECT new_value_minor_units, offer_price_minor_units
			FROM course_price_changes
			WHERE course_id=c.id AND section_id IS NULL
			ORDER BY changed_at DESC, id DESC LIMIT 1
		) price ON TRUE
		WHERE `+where+`
		ORDER BY c.id
		LIMIT $2 OFFSET $3
	`, query, BundleCoursePickerPageSize, (page-1)*BundleCoursePickerPageSize)
	if err != nil {
		return BundleCoursePage{}, fmt.Errorf("listing eligible Bundle Courses: %w", err)
	}
	defer rows.Close()
	items, err := scanBundleCourseOptions(rows)
	if err != nil {
		return BundleCoursePage{}, err
	}

	return BundleCoursePage{
		Items: items, Page: page, PageSize: BundleCoursePickerPageSize,
		Total: total, HasNext: page*BundleCoursePickerPageSize < total,
	}, nil
}
