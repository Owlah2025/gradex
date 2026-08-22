//go:build integration

package academic

import (
	"context"
	"errors"
	"testing"
	"time"
)

// D-093 §7 (amended) — the normalized official code is part of canonical Subject
// identity and is immutable once established.
//
// T4-A closed reuse from two directions: a second Subject cannot take a retired
// Subject's code, and a retired Subject cannot release its own. This file closes
// the third: an ACTIVE Subject renumbering itself, which would free the old
// normalized code for a different Subject and silently perform academic
// renumbering through an ordinary Admin edit.
//
// Display formatting stays editable, because "0418 320" and "0418-320" are the
// same identity — only the form a Student reads differs.

type codeIdentityFixture struct {
	fx    *profileFixture
	ctx   context.Context
	admin Actor
}

func newCodeIdentityFixture(t *testing.T) *codeIdentityFixture {
	t.Helper()
	fx := newProfileFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	var adminID string
	if err := fx.pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t4a1-admin@example.test', 't4a1-admin@example.test', 'ADMIN', 'ACTIVE', 'T4A1 Admin')
		RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	return &codeIdentityFixture{fx: fx, ctx: ctx, admin: Actor{AdminAccountID: adminID, ActorDescriptor: adminID}}
}

func (c *codeIdentityFixture) normalizedCode(t *testing.T, subjectID string) *string {
	t.Helper()
	var normalized *string
	if err := c.fx.pool.QueryRow(c.ctx,
		`SELECT code_normalized FROM subjects WHERE id = $1::uuid`, subjectID).Scan(&normalized); err != nil {
		t.Fatalf("reading code_normalized: %v", err)
	}
	return normalized
}

// 1–4, 7. The identity rule on an active coded Subject.
func TestActiveSubjectNormalizedCodeIsImmutable(t *testing.T) {
	f := newCodeIdentityFixture(t)

	subject, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418-320"),
		TitleAr:      "مبادئ نظم الحاسوب", TitleEn: "Principles of Computer Systems",
	})
	if err != nil {
		t.Fatalf("creating subject: %v", err)
	}
	if got := f.normalizedCode(t, subject.ID); got == nil || *got != "0418320" {
		t.Fatalf("code_normalized = %v, want 0418320", got)
	}

	// 2/3. Formatting-only correction is allowed and preserves identity.
	reformatted, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: subject.ID, OfficialCode: strptr("0418 320"),
	})
	if err != nil {
		t.Fatalf("formatting-only correction must be allowed: %v", err)
	}
	if reformatted.OfficialCode == nil || *reformatted.OfficialCode != "0418 320" {
		t.Fatalf("display form was not updated: %v", reformatted.OfficialCode)
	}
	if got := f.normalizedCode(t, subject.ID); got == nil || *got != "0418320" {
		t.Fatalf("formatting-only edit changed identity: code_normalized = %v", got)
	}
	// Lower-case and punctuation variants are the same identity too.
	for _, form := range []string{"0418-320", "0418.320", "0418320"} {
		if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
			Actor: f.admin, SubjectID: subject.ID, OfficialCode: strptr(form),
		}); err != nil {
			t.Fatalf("formatting variant %q refused: %v", form, err)
		}
		if got := f.normalizedCode(t, subject.ID); got == nil || *got != "0418320" {
			t.Fatalf("variant %q changed identity: %v", form, got)
		}
	}

	// 4. A different normalized identity is refused, even with titles unchanged.
	for _, renumber := range []string{"0418-321", "0418-999", "CS320"} {
		if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
			Actor: f.admin, SubjectID: subject.ID, OfficialCode: strptr(renumber),
		}); !errors.Is(err, ErrSubjectCodeImmutable) {
			t.Fatalf("renumber to %q: got %v, want ErrSubjectCodeImmutable", renumber, err)
		}
	}

	// 7. Coded → NULL is refused: a canonical identity cannot be withdrawn.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: subject.ID, ClearCode: true,
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("clearing an established code: got %v, want ErrSubjectCodeImmutable", err)
	}

	// A rejected renumber leaves no partial mutation: the title supplied in the
	// same call must not have landed.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: subject.ID,
		OfficialCode: strptr("0418-777"), TitleEn: strptr("Should Not Persist"),
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("combined renumber+rename: got %v, want ErrSubjectCodeImmutable", err)
	}
	var titleAfter string
	if err := f.fx.pool.QueryRow(f.ctx,
		`SELECT title_en FROM subjects WHERE id = $1::uuid`, subject.ID).Scan(&titleAfter); err != nil {
		t.Fatalf("re-reading subject: %v", err)
	}
	if titleAfter == "Should Not Persist" {
		t.Fatalf("a refused renumber partially applied the title change")
	}

	// Titles remain freely editable — only the code is identity.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: subject.ID, TitleEn: strptr("Principles of Computer Systems (rev)"),
	}); err != nil {
		t.Fatalf("title edit refused: %v", err)
	}
}

// 6. The database refuses the same change with the domain bypassed entirely.
func TestActiveSubjectNormalizedCodeIsImmutableInTheDatabase(t *testing.T) {
	f := newCodeIdentityFixture(t)

	subject, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418-320"),
		TitleAr:      "مبادئ", TitleEn: "Principles",
	})
	if err != nil {
		t.Fatalf("creating subject: %v", err)
	}

	if _, err := f.fx.pool.Exec(f.ctx,
		`UPDATE subjects SET official_code = '0418-999' WHERE id = $1::uuid`, subject.ID); err == nil {
		t.Fatalf("direct SQL renumber must be refused by the database")
	}
	if _, err := f.fx.pool.Exec(f.ctx,
		`UPDATE subjects SET official_code = NULL WHERE id = $1::uuid`, subject.ID); err == nil {
		t.Fatalf("direct SQL code clearing must be refused by the database")
	}
	// Formatting-only stays possible at the database layer too, so the guard is
	// about identity rather than about the column being frozen.
	if _, err := f.fx.pool.Exec(f.ctx,
		`UPDATE subjects SET official_code = '0418 320' WHERE id = $1::uuid`, subject.ID); err != nil {
		t.Fatalf("direct SQL formatting-only correction refused: %v", err)
	}
	if got := f.normalizedCode(t, subject.ID); got == nil || *got != "0418320" {
		t.Fatalf("code_normalized drifted to %v", got)
	}
}

// 9. The old normalized code never becomes available through any edit path.
func TestOldNormalizedCodeNeverBecomesAvailable(t *testing.T) {
	f := newCodeIdentityFixture(t)

	original, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418-320"), TitleAr: "أ", TitleEn: "Original",
	})
	if err != nil {
		t.Fatalf("creating subject: %v", err)
	}

	// Every route that could have freed 0418320.
	_, _ = f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: original.ID, OfficialCode: strptr("0418-999")})
	_, _ = f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: original.ID, ClearCode: true})
	if _, err := f.fx.repo.RetireSubject(f.ctx, RetireRequest{Actor: f.admin, ID: original.ID}); err != nil {
		t.Fatalf("retiring subject: %v", err)
	}
	_, _ = f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: original.ID, ClearCode: true})

	// 11. The reservation still holds after all of it.
	_, err = f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418320"), TitleAr: "ب", TitleEn: "Claimant",
	})
	if err == nil {
		t.Fatalf("0418320 became available after edit and retirement attempts")
	}
	var duplicate *DuplicateSubjectError
	if !errors.As(err, &duplicate) || duplicate.Existing == nil || duplicate.Existing.ID != original.ID {
		t.Fatalf("conflict must still name the original holder, got %v", err)
	}

	// 8. And the retired Subject is still immutable.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: original.ID, OfficialCode: strptr("0418-111"),
	}); err == nil {
		t.Fatalf("retired subject code must stay immutable")
	}
}

// 12. Codeless → coded: a codeless Subject may receive its FIRST code, after
// which the same immutability applies.
func TestCodelessSubjectMayReceiveItsFirstCodeThenBecomesImmutable(t *testing.T) {
	f := newCodeIdentityFixture(t)

	codeless, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		TitleAr: "مواضيع خاصة", TitleEn: "Special Topics",
	})
	if err != nil {
		t.Fatalf("creating codeless subject: %v", err)
	}
	if got := f.normalizedCode(t, codeless.ID); got != nil {
		t.Fatalf("codeless subject has code_normalized = %v", got)
	}

	// The first code is accepted.
	coded, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: codeless.ID, OfficialCode: strptr("0418-450"),
	})
	if err != nil {
		t.Fatalf("codeless subject must accept its first code: %v", err)
	}
	if coded.OfficialCode == nil || *coded.OfficialCode != "0418-450" {
		t.Fatalf("first code was not stored: %v", coded.OfficialCode)
	}

	// And from then on it is identity, like any other coded Subject.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: codeless.ID, OfficialCode: strptr("0418-451"),
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("second code change: got %v, want ErrSubjectCodeImmutable", err)
	}
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: codeless.ID, ClearCode: true,
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("clearing a newly established code: got %v, want ErrSubjectCodeImmutable", err)
	}

	// A first code that collides with a reserved one is refused, including one
	// reserved by a retired Subject.
	reserved, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418-500"), TitleAr: "م", TitleEn: "Reserved Holder",
	})
	if err != nil {
		t.Fatalf("creating reserved subject: %v", err)
	}
	if _, err := f.fx.repo.RetireSubject(f.ctx, RetireRequest{Actor: f.admin, ID: reserved.ID}); err != nil {
		t.Fatalf("retiring reserved subject: %v", err)
	}
	another, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		TitleAr: "مواضيع ثانية", TitleEn: "Other Special Topics",
	})
	if err != nil {
		t.Fatalf("creating second codeless subject: %v", err)
	}
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: another.ID, OfficialCode: strptr("0418500"),
	}); err == nil {
		t.Fatalf("a first code colliding with a retired Subject's reservation must be refused")
	}

	// A codeless Subject that stays codeless keeps its editable title identity.
	if _, err := f.fx.repo.UpdateSubject(f.ctx, UpdateSubjectRequest{
		Actor: f.admin, SubjectID: another.ID, TitleEn: strptr("Renamed Special Topics"),
	}); err != nil {
		t.Fatalf("codeless subject title must stay editable: %v", err)
	}
}

// 10. The reservation is scoped to the Institution, never global.
func TestSubjectCodeIdentityIsScopedToInstitution(t *testing.T) {
	f := newCodeIdentityFixture(t)

	if _, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: f.fx.institution,
		OfficialCode: strptr("0418-320"), TitleAr: "أ", TitleEn: "Home Institution",
	}); err != nil {
		t.Fatalf("creating subject: %v", err)
	}

	var otherInstitution string
	if err := f.fx.pool.QueryRow(f.ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 't4a1-other-university', 'جامعة أخرى', 'Other University')
		RETURNING id::text`).Scan(&otherInstitution); err != nil {
		t.Fatalf("seeding other institution: %v", err)
	}
	if _, err := f.fx.repo.CreateSubject(f.ctx, CreateSubjectRequest{
		Actor: f.admin, InstitutionID: otherInstitution,
		OfficialCode: strptr("0418-320"), TitleAr: "ب", TitleEn: "Other Institution",
	}); err != nil {
		t.Fatalf("the same code in another Institution must remain valid: %v", err)
	}
}
