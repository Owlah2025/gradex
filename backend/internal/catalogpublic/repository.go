package catalogpublic

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const currencyKWD = "KWD"

var studyYearLabels = map[string][2]string{
	"PREP":   {"تمهيدي", "Preparatory"},
	"YEAR_1": {"السنة الأولى", "Year 1"},
	"YEAR_2": {"السنة الثانية", "Year 2"},
	"YEAR_3": {"السنة الثالثة", "Year 3"},
	"YEAR_4": {"السنة الرابعة", "Year 4"},
}

type Taxonomy struct {
	Label string  `json:"label"`
	Code  *string `json:"code,omitempty"`
}

type Price struct {
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
}

type Course struct {
	ID                    string    `json:"id"`
	Slug                  string    `json:"slug"`
	Title                 string    `json:"title"`
	InstructorDisplayName string    `json:"instructor_display_name"`
	Major                 *Taxonomy `json:"major,omitempty"`
	Subject               *Taxonomy `json:"subject,omitempty"`
	StudyYear             *Taxonomy `json:"study_year,omitempty"`
	Price                 *Price    `json:"price,omitempty"`
	HasPreview            bool      `json:"has_preview"`
}

type Section struct {
	Title       string `json:"title"`
	Position    int    `json:"position"`
	LessonCount int    `json:"lesson_count"`
}

type DetailCourse struct {
	Course
	Description string    `json:"description"`
	Sections    []Section `json:"sections"`
}

type ListResult struct {
	Items    []Course `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int      `json:"total"`
}

var (
	ErrRepositoryNil = errors.New("database pool is required")
	ErrVisibilityNil = errors.New("published-only visibility predicate is required")
)

// Repository is the public catalogue read boundary. Its later list and detail
// consumers must obtain rows through visibility; construction refuses a
// missing policy so a query cannot silently fall back to unfiltered data.
type Repository struct {
	pool       *pgxpool.Pool
	visibility VisibilityPredicate
}

func NewRepository(pool *pgxpool.Pool, visibility VisibilityPredicate) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	if visibility == nil {
		return nil, ErrVisibilityNil
	}
	return &Repository{pool: pool, visibility: visibility}, nil
}

// List validates the public visibility boundary for the list route. T010 owns
// the list projection, so this foundation deliberately returns no catalogue
// fields yet.
func (r *Repository) List(ctx context.Context, arabic bool, page, pageSize int) (ListResult, error) {
	query := r.projectionQuery(r.visibility("c", "cr"), ``, `ORDER BY c.id LIMIT $2 OFFSET $3`)
	rows, err := r.pool.Query(ctx, query, arabic, pageSize, (page-1)*pageSize)
	if err != nil {
		return ListResult{}, fmt.Errorf("listing public courses: %w", err)
	}
	defer rows.Close()
	items, err := scanCourses(rows, arabic)
	if err != nil {
		return ListResult{}, err
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM (`+r.visibleCourseQuery()+`) visible`).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("counting public courses: %w", err)
	}
	return ListResult{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (r *Repository) Search(ctx context.Context, arabic bool, page, pageSize int, searchQuery string) (ListResult, error) {
	visibility := r.visibility("c", "cr")
	query := r.projectionQuery(visibility, ``, `AND `+SearchMatchPredicate("$2")+` ORDER BY c.id LIMIT $3 OFFSET $4`)
	rows, err := r.pool.Query(ctx, query, arabic, searchQuery, pageSize, (page-1)*pageSize)
	if err != nil {
		return ListResult{}, fmt.Errorf("searching public courses: %w", err)
	}
	defer rows.Close()
	items, err := scanCourses(rows, arabic)
	if err != nil {
		return ListResult{}, err
	}
	var total int
	if err := r.pool.QueryRow(ctx, r.searchCountQuery(visibility), searchQuery).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("counting public search results: %w", err)
	}
	return ListResult{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (r *Repository) searchCountQuery(visibility string) string {
	return `SELECT count(*)
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		JOIN accounts a ON a.id = c.owner_account_id
		LEFT JOIN taxonomy_terms major ON major.id = cr.major_term_id
		LEFT JOIN taxonomy_terms subject ON subject.id = cr.subject_term_id
		WHERE ` + visibility + ` AND ` + SearchMatchPredicate("$1")
}

// SearchMatchPredicate is the exact three-way predicate used by Search. It
// keeps the stored revision document and joined display fields on the same
// normalized input while PublishedOnly remains the sole visibility authority.
func SearchMatchPredicate(queryParameter string) string {
	normalizedQuery := "catalog_normalize_ar(" + queryParameter + ")"
	joinedFields := "catalog_normalize_ar(concat_ws(' ', a.display_name, major.label_ar, major.label_en, major.academic_code, subject.label_ar, subject.label_en, subject.academic_code, cr.study_year::text))"
	return "(" + normalizedQuery + " = '' OR cr.search_text LIKE '%' || " + normalizedQuery + " || '%' OR " + joinedFields + " LIKE '%' || " + normalizedQuery + " || '%')"
}

// Detail reports whether an exact Course identifier is public. The identifier
// and every visibility exclusion share one SQL boundary, so callers cannot
// fetch a Course and make a separate visibility decision in application code.
func (r *Repository) Detail(ctx context.Context, identifier string, arabic bool) (*DetailCourse, error) {
	rows, err := r.pool.Query(ctx, r.projectionQuery(r.visibility("c", "cr"), publicCourseIdentifierPredicate(identifier), ``), arabic, identifier)
	if err != nil {
		return nil, fmt.Errorf("looking up public course: %w", err)
	}
	defer rows.Close()
	items, err := scanCourses(rows, arabic)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	description, sections, found, err := r.detailContent(ctx, items[0].ID, arabic)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &DetailCourse{Course: items[0], Description: description, Sections: sections}, nil
}

func (r *Repository) detailContent(ctx context.Context, courseID string, arabic bool) (string, []Section, bool, error) {
	description, found, err := r.description(ctx, courseID, arabic)
	if err != nil || !found {
		return description, nil, found, err
	}
	sections, found, err := r.sections(ctx, courseID, arabic)
	if err != nil || !found {
		return "", nil, found, err
	}
	return description, sections, true, nil
}

func publicCourseIdentifierPredicate(identifier string) string {
	parsed, err := uuid.Parse(identifier)
	if err == nil && parsed.String() == identifier {
		return `AND c.id = $2::uuid`
	}
	return `AND c.slug = $2`
}

func (r *Repository) projectionQuery(visibility, identifier, suffix string) string {
	return `SELECT c.id::text, c.slug,
		CASE WHEN $1 THEN cr.title_ar ELSE cr.title_en END,
		a.display_name,
		CASE WHEN major.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN major.label_ar ELSE major.label_en END END, NULL::text,
		CASE WHEN subject.id IS NULL THEN NULL ELSE CASE WHEN $1 THEN subject.label_ar ELSE subject.label_en END END, subject.academic_code,
		cr.study_year::text,
		price.new_value_minor_units,
		cr.preview_asset_version_id IS NOT NULL
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		JOIN accounts a ON a.id = c.owner_account_id
		LEFT JOIN taxonomy_terms major ON major.id = cr.major_term_id
		LEFT JOIN taxonomy_terms subject ON subject.id = cr.subject_term_id
		LEFT JOIN LATERAL (SELECT new_value_minor_units FROM course_price_changes WHERE course_id = c.id AND section_id IS NULL ORDER BY changed_at DESC, id DESC LIMIT 1) price ON true
		WHERE ` + visibility + ` ` + identifier + ` ` + suffix
}

func scanCourses(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, arabic bool) ([]Course, error) {
	var items []Course
	for rows.Next() {
		var item Course
		var majorLabel, majorCode, subjectLabel, subjectCode, studyYear *string
		var amount *int64
		if err := rows.Scan(&item.ID, &item.Slug, &item.Title, &item.InstructorDisplayName, &majorLabel, &majorCode, &subjectLabel, &subjectCode, &studyYear, &amount, &item.HasPreview); err != nil {
			return nil, fmt.Errorf("scanning public course: %w", err)
		}
		if majorLabel != nil {
			item.Major = &Taxonomy{Label: *majorLabel, Code: majorCode}
		}
		if subjectLabel != nil {
			item.Subject = &Taxonomy{Label: *subjectLabel, Code: subjectCode}
		}
		if studyYear != nil {
			if label, ok := localizedStudyYear(*studyYear, arabic); ok {
				item.StudyYear = &Taxonomy{Label: label}
			}
		}
		if amount != nil {
			item.Price = &Price{MinorUnits: *amount, Currency: currencyKWD}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading public courses: %w", err)
	}
	return items, nil
}

func localizedStudyYear(value string, arabic bool) (string, bool) {
	label, ok := studyYearLabels[value]
	if !ok {
		return "", false
	}
	if arabic {
		return label[0], true
	}
	return label[1], true
}

func (r *Repository) description(ctx context.Context, courseID string, arabic bool) (string, bool, error) {
	var description string
	err := r.pool.QueryRow(ctx, `SELECT CASE WHEN $1 THEN cr.description_ar ELSE cr.description_en END
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE `+r.visibility("c", "cr")+` AND c.id = $2::uuid`, arabic, courseID).Scan(&description)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("loading public course description: %w", err)
	}
	return description, true, nil
}

func (r *Repository) sections(ctx context.Context, courseID string, arabic bool) ([]Section, bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT cs.id::text,
		CASE WHEN $1 THEN cs.title_ar ELSE cs.title_en END,
		cs.position,
		count(cl.id)
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		LEFT JOIN course_sections cs ON cs.revision_id = cr.id
		LEFT JOIN course_lessons cl ON cl.section_id = cs.id
		WHERE `+r.visibility("c", "cr")+` AND c.id = $2::uuid
		GROUP BY cs.id
		ORDER BY min(cs.position)`, arabic, courseID)
	if err != nil {
		return nil, false, fmt.Errorf("listing public sections: %w", err)
	}
	defer rows.Close()
	sections := make([]Section, 0)
	found := false
	for rows.Next() {
		found = true
		section, exists, err := scanPublicSection(rows)
		if err != nil {
			return nil, false, fmt.Errorf("scanning public section: %w", err)
		}
		if !exists {
			continue
		}
		sections = append(sections, section)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("reading public sections: %w", err)
	}
	return sections, found, nil
}

func scanPublicSection(row interface{ Scan(...any) error }) (Section, bool, error) {
	var sectionID, title *string
	var position *int
	var section Section
	if err := row.Scan(&sectionID, &title, &position, &section.LessonCount); err != nil {
		return Section{}, false, err
	}
	if sectionID == nil {
		return Section{}, false, nil
	}
	section.Title = *title
	section.Position = *position
	return section, true, nil
}

func (r *Repository) visibleCourseQuery() string {
	return `SELECT 1
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE ` + r.visibility("c", "cr")
}
