//go:build !production

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Extra Universities for the local Student preview stack, and for nothing else.
//
// The ordinary seed carries two institutions. That is the right number for the
// E2E suite — every spec that counts institutions, asserts a filter's options,
// or walks the academic picker is written against it — but it is too few to
// exercise the landing page's University rail, which only shows its previous and
// next controls once the rail actually overflows.
//
// So these rows are added by an explicit verb rather than by the seed. Nothing
// reaches them unless `-preview-institutions` is passed, which only
// `scripts/student-preview.sh` does; `go test ./...`, the Playwright harness and
// every existing spec see exactly the fixture they saw before.
//
// The names are real Kuwaiti institutions chosen for their shape rather than for
// their accuracy as a catalogue: the set deliberately mixes a 17-character name
// with a 53-character one, in both scripts, so that chip widths and the rail's
// snap points are exercised by something closer to the worst case than to the
// average one. They are demonstration fixtures for a throwaway local database,
// and they carry a `preview-` slug prefix so they can never be mistaken for
// catalogue data.
const previewInstitutionSlugPrefix = "preview-"

type previewInstitution struct {
	slug    string
	nameAr  string
	nameEn  string
	program previewProgram
}

type previewProgram struct {
	slug   string
	nameAr string
	nameEn string
}

// Kuwait University is deliberately absent: the ordinary seed already creates it,
// and the point of the Kuwait-first ordering is that it sorts ahead of rows like
// these, several of which precede it alphabetically.
var previewInstitutions = []previewInstitution{
	{
		slug:   previewInstitutionSlugPrefix + "aum",
		nameAr: "الجامعة الأمريكية في الشرق الأوسط",
		nameEn: "American University of the Middle East",
		program: previewProgram{
			slug: "mechanical-engineering", nameAr: "الهندسة الميكانيكية", nameEn: "Mechanical Engineering",
		},
	},
	{
		slug:   previewInstitutionSlugPrefix + "auk",
		nameAr: "الجامعة الأمريكية في الكويت",
		nameEn: "American University of Kuwait",
		program: previewProgram{
			slug: "business-administration", nameAr: "إدارة الأعمال", nameEn: "Business Administration",
		},
	},
	{
		slug:   previewInstitutionSlugPrefix + "gust",
		nameAr: "جامعة الخليج للعلوم والتكنولوجيا",
		nameEn: "Gulf University for Science and Technology",
		program: previewProgram{
			slug: "computer-engineering", nameAr: "هندسة الحاسوب", nameEn: "Computer Engineering",
		},
	},
	{
		// The longest name in the set, in both scripts.
		slug:   previewInstitutionSlugPrefix + "paaet",
		nameAr: "الهيئة العامة للتعليم التطبيقي والتدريب",
		nameEn: "Public Authority for Applied Education and Training",
		program: previewProgram{
			slug: "applied-sciences", nameAr: "العلوم التطبيقية", nameEn: "Applied Sciences",
		},
	},
	{
		// The shortest, so the rail is not a row of uniform chips.
		slug:   previewInstitutionSlugPrefix + "kilaw",
		nameAr: "كلية القانون الكويتية",
		nameEn: "Kuwait Law School",
		program: previewProgram{
			slug: "law", nameAr: "القانون", nameEn: "Law",
		},
	},
	{
		slug:   previewInstitutionSlugPrefix + "box-hill",
		nameAr: "كلية بوكس هل الكويت",
		nameEn: "Box Hill College Kuwait",
		program: previewProgram{
			slug: "design", nameAr: "التصميم", nameEn: "Design",
		},
	},
	{
		slug:   previewInstitutionSlugPrefix + "ack",
		nameAr: "الكلية الأسترالية في الكويت",
		nameEn: "Australian College of Kuwait",
		program: previewProgram{
			slug: "aviation", nameAr: "الطيران", nameEn: "Aviation",
		},
	},
}

// seedPreviewInstitutions adds the demonstration Universities to an already
// seeded preview database.
//
// Idempotent, because the preview stack is long-lived and this verb is expected
// to be run again after a re-seed without anyone checking first.
func seedPreviewInstitutions(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	added := 0
	for _, institution := range previewInstitutions {
		var institutionID string
		// Both uniqueness guarantees involved here are partial indexes predicated
		// on "retired_at IS NULL", so each ON CONFLICT repeats that predicate:
		// without it Postgres cannot infer the arbiter index and rejects the
		// statement outright.
		err := pool.QueryRow(ctx, `
			INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
			VALUES ('KW', $1, $2, $3, 5)
			ON CONFLICT (slug) WHERE retired_at IS NULL
			DO UPDATE SET name_ar = EXCLUDED.name_ar, name_en = EXCLUDED.name_en
			RETURNING id::text`,
			institution.slug, institution.nameAr, institution.nameEn,
		).Scan(&institutionID)
		if err != nil {
			return added, fmt.Errorf("seeding preview institution %s: %w", institution.slug, err)
		}

		// A University with no Program strands the academic picker on its second
		// question, which would make the rail look finished and the flow behind
		// it look broken.
		if _, err := pool.Exec(ctx, `
			INSERT INTO programs (institution_id, slug, name_ar, name_en, degree_kind)
			VALUES ($1::uuid, $2, $3, $4, 'BSC')
			ON CONFLICT (institution_id, slug) WHERE retired_at IS NULL DO NOTHING`,
			institutionID, institution.program.slug, institution.program.nameAr, institution.program.nameEn,
		); err != nil {
			return added, fmt.Errorf("seeding preview program for %s: %w", institution.slug, err)
		}
		added++
	}
	return added, nil
}
