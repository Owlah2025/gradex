package catalogpublic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Public Subject discovery (D-106 §5).
//
// # HOW THIS DIFFERS FROM academic_options.go
//
// That file lists Subjects as *filter values* — the narrowing a visitor applies
// to a set of Courses. This file presents Subjects as *entities in their own
// right*, including the ones no Course teaches yet, because an unserved Subject
// is the thing D-106 exists to surface. The two are not merged: a filter list
// must never offer a value that narrows every Course away, and this list must
// never hide a Subject for having no Course.
//
// # WHAT A SUBJECT IS NOT
//
// A Subject is not a product. Nothing here carries a price, an entitlement, a
// purchase affordance, or a lifecycle. When a Subject is served, this read hands
// back the slug of the real published Course so the caller can link to it; when
// it is not, it hands back nothing and the caller offers demand registration.
// PublishedOnly remains the sole publication authority in both cases — an
// unpublished, suspended, or retired Course leaves its Subject unserved, exactly
// as it leaves it absent from the catalogue.

// SubjectCourseRef points at one published Course that teaches a Subject.
type SubjectCourseRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// SubjectListing is one Subject as the public catalogue presents it.
type SubjectListing struct {
	// SubjectID is the canonical Subject identifier. It is exposed because it
	// is the only value the demand endpoint accepts, and discovery is the only
	// place a client can learn it: without it here, a Student who found an
	// unserved Subject through the public catalogue would have no way to
	// register demand against it.
	//
	// Publishing it costs nothing. A Subject identifier authorises nothing, is
	// already derivable from the code-less branch of Value, and names a row
	// whose whole purpose is to be publicly discoverable.
	SubjectID string `json:"subject_id"`
	// Value is the public, shareable identifier: the official code when the
	// Subject has one, its identifier otherwise. Same authority as
	// SubjectFilterOption.Value, so a link built from either resolves here.
	// It is what belongs in a URL; SubjectID is what belongs in a demand call.
	Value   string `json:"value"`
	Code    string `json:"code,omitempty"`
	TitleAr string `json:"title_ar"`
	TitleEn string `json:"title_en"`

	InstitutionSlug   string `json:"institution_slug"`
	InstitutionNameAr string `json:"institution_name_ar"`
	InstitutionNameEn string `json:"institution_name_en"`

	// Served is true when at least one published Course teaches this Subject.
	// It is derived from Courses at read time, never stored: a Course that is
	// published or suspended between two reads changes this answer, and a
	// cached flag would be wrong the moment it did.
	Served bool `json:"served"`
	// Courses is non-empty exactly when Served is true.
	Courses []SubjectCourseRef `json:"courses"`
}

// SubjectPage is one page of Subject listings.
type SubjectPage struct {
	Items    []SubjectListing `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
}

// SubjectQuery narrows a Subject listing. Every field is optional; the zero
// value browses every Subject of every active Institution.
type SubjectQuery struct {
	InstitutionSlug string
	// Search matches the Subject's code or either title. Free text.
	Search string
	// ServedOnly and UnservedOnly are mutually exclusive; setting both returns
	// an empty page rather than silently preferring one.
	ServedOnly   bool
	UnservedOnly bool
	Page         int
	PageSize     int
}

// subjectServedCourses is the lateral join that resolves a Subject to the
// published Courses teaching it. It is built from PublishedOnly so this path
// cannot drift from the rest of the catalogue's visibility rule.
func subjectServedCourses(arabic string) string {
	return fmt.Sprintf(`
		LEFT JOIN LATERAL (
			SELECT json_agg(
				json_build_object(
					'slug', c.slug,
					'title', CASE WHEN %s THEN cr.title_ar ELSE cr.title_en END
				) ORDER BY c.slug
			) AS courses
			FROM courses c
			JOIN course_revisions cr ON cr.course_id = c.id
			WHERE c.subject_id = s.id AND %s
		) served ON TRUE`, arabic, PublishedOnly("c", "cr"))
}

// BrowseSubjects lists Subjects with their service status.
//
// An unknown Institution slug yields an empty page rather than an error, for the
// same reason the filter lists do: a stale shared link is an ordinary empty
// state, not a failure.
func (r *Repository) BrowseSubjects(
	ctx context.Context, query SubjectQuery, arabic bool,
) (SubjectPage, error) {
	page, pageSize := query.Page, query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 40
	}
	result := SubjectPage{Items: []SubjectListing{}, Page: page, PageSize: pageSize}

	// Contradictory narrowing is answered with nothing, not with a guess about
	// which half the caller meant.
	if query.ServedOnly && query.UnservedOnly {
		return result, nil
	}

	arguments := []any{arabic}
	add := func(value any) string {
		arguments = append(arguments, value)
		return fmt.Sprintf("$%d", len(arguments))
	}

	clauses := []string{"i.retired_at IS NULL", "s.retired_at IS NULL"}
	if slug := strings.TrimSpace(query.InstitutionSlug); slug != "" {
		clauses = append(clauses, "i.slug = "+add(slug))
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		// ILIKE over the stored forms plus the normalized code, so "cs101",
		// "CS 101" and "cs-101" all reach the same Subject.
		pattern := add("%" + search + "%")
		normalized := add("%" + normalizeSubjectSearchCode(search) + "%")
		clauses = append(clauses, fmt.Sprintf(
			"(s.title_en ILIKE %s OR s.title_ar ILIKE %s OR s.official_code ILIKE %s OR s.code_normalized LIKE %s)",
			pattern, pattern, pattern, normalized))
	}
	if query.ServedOnly {
		clauses = append(clauses, "served.courses IS NOT NULL")
	}
	if query.UnservedOnly {
		clauses = append(clauses, "served.courses IS NULL")
	}

	where := "WHERE " + strings.Join(clauses, " AND ")
	from := `
		FROM subjects s
		JOIN institutions i ON i.id = s.institution_id` + subjectServedCourses("$1") + `
		` + where

	if err := r.pool.QueryRow(ctx, "SELECT count(*) "+from, arguments...).
		Scan(&result.Total); err != nil {
		return SubjectPage{}, fmt.Errorf("counting public subjects: %w", err)
	}
	if result.Total == 0 {
		return result, nil
	}

	limit := add(pageSize)
	offset := add((page - 1) * pageSize)
	// Unserved Subjects sort after served ones within an Institution: a visitor
	// scanning a university's page should meet what Gradex actually teaches
	// before what it does not.
	listing := `
		SELECT s.id::text, COALESCE(s.official_code, ''), s.title_ar, s.title_en,
			i.slug, i.name_ar, i.name_en,
			COALESCE(served.courses, '[]'::json)::text ` + from + `
		ORDER BY i.name_en ASC, (served.courses IS NULL) ASC,
			COALESCE(s.official_code, s.title_en) ASC
		LIMIT ` + limit + ` OFFSET ` + offset

	rows, err := r.pool.Query(ctx, listing, arguments...)
	if err != nil {
		return SubjectPage{}, fmt.Errorf("listing public subjects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var identifier, code, coursesJSON string
		var item SubjectListing
		if err := rows.Scan(&identifier, &code, &item.TitleAr, &item.TitleEn,
			&item.InstitutionSlug, &item.InstitutionNameAr, &item.InstitutionNameEn,
			&coursesJSON); err != nil {
			return SubjectPage{}, fmt.Errorf("scanning public subject: %w", err)
		}
		item.SubjectID = identifier
		item.Code = code
		item.Value = code
		if code == "" {
			item.Value = identifier
		}
		item.Courses = []SubjectCourseRef{}
		if err := json.Unmarshal([]byte(coursesJSON), &item.Courses); err != nil {
			return SubjectPage{}, fmt.Errorf("decoding subject courses: %w", err)
		}
		item.Served = len(item.Courses) > 0
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// SubjectDetail resolves one Subject within one Institution by its public value,
// which is its official code or, for a code-less Subject, its identifier.
//
// Returns nil when nothing matches, so the caller answers 404 rather than
// inventing an empty Subject.
func (r *Repository) SubjectDetail(
	ctx context.Context, institutionSlug, value string, arabic bool,
) (*SubjectListing, error) {
	institutionSlug = strings.TrimSpace(institutionSlug)
	value = strings.TrimSpace(value)
	if institutionSlug == "" || value == "" {
		return nil, nil
	}

	// Whether this value is a Subject identifier is decided by parsing it as a
	// UUID in Go, never by a shape test in SQL.
	//
	// The previous form matched `^[0-9a-fA-F-]{36}$` and then cast the raw value
	// with ::uuid. A string of 36 hyphens satisfies that pattern and is not a
	// UUID, so PostgreSQL raised invalid_text_representation and the repository
	// error surfaced as a 500 — a malformed URL answered as a server fault. No
	// regex fixes this, because the shape is not the question: only a parser
	// knows what PostgreSQL will accept.
	//
	// The parsed canonical value is bound as its own parameter. A value that is
	// not a UUID binds NULL, so the identifier branch cannot match and the
	// lookup falls through to the ordinary code path; finding nothing there is
	// an ordinary not-found, which the HTTP layer renders as 404.
	var subjectID any
	if parsed, err := uuid.Parse(value); err == nil {
		subjectID = parsed.String()
	}

	// A code and an identifier are matched in one query rather than by guessing
	// which one the caller meant: a Subject whose official code happens to parse
	// as a UUID must still resolve by code.
	query := `
		SELECT s.id::text, COALESCE(s.official_code, ''), s.title_ar, s.title_en,
			i.slug, i.name_ar, i.name_en,
			COALESCE(served.courses, '[]'::json)::text
		FROM subjects s
		JOIN institutions i ON i.id = s.institution_id` + subjectServedCourses("$1") + `
		WHERE i.retired_at IS NULL AND s.retired_at IS NULL
			AND i.slug = $2
			AND (
				s.code_normalized = academic_normalize_code($3)
				OR ($4::uuid IS NOT NULL AND s.id = $4::uuid)
			)
		LIMIT 1`

	var identifier, code, coursesJSON string
	var item SubjectListing
	err := r.pool.QueryRow(ctx, query, arabic, institutionSlug, value, subjectID).Scan(
		&identifier, &code, &item.TitleAr, &item.TitleEn,
		&item.InstitutionSlug, &item.InstitutionNameAr, &item.InstitutionNameEn, &coursesJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading public subject: %w", err)
	}
	item.SubjectID = identifier
	item.Code = code
	item.Value = code
	if code == "" {
		item.Value = identifier
	}
	item.Courses = []SubjectCourseRef{}
	if err := json.Unmarshal([]byte(coursesJSON), &item.Courses); err != nil {
		return nil, fmt.Errorf("decoding subject courses: %w", err)
	}
	item.Served = len(item.Courses) > 0
	return &item, nil
}

// normalizeSubjectSearchCode mirrors academic_normalize_code for the search
// path so "CS 101" typed by a Student matches the stored CS101 identity.
func normalizeSubjectSearchCode(input string) string {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		}
	}
	return b.String()
}
