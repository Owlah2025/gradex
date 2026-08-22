//go:build integration

package academic

import (
	"context"
	"errors"
	"testing"
	"time"
)

// D-093 §7 — official Subject code permanence, domain half.
//
// Migration 0025 widens the coded-Subject unique index to span active AND
// retired rows, which stops a SECOND Subject from claiming a retired Subject's
// code. The index alone cannot stop the reservation being released from the
// other direction — by editing the retired Subject's own code — so that is
// refused here. Without this the Founder decision would have a one-call bypass.

func TestSubjectRetiredCodeStaysReserved(t *testing.T) {
	fx := newProfileFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var adminID string
	if err := fx.pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t4a-admin@example.test', 't4a-admin@example.test', 'ADMIN', 'ACTIVE', 'T4A Admin')
		RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	admin := Actor{AdminAccountID: adminID, ActorDescriptor: adminID}

	original, err := fx.repo.CreateSubject(ctx, CreateSubjectRequest{
		Actor: admin, InstitutionID: fx.institution,
		OfficialCode: strptr("0418-320"),
		TitleAr:      "مبادئ نظم الحاسوب", TitleEn: "Principles of Computer Systems",
	})
	if err != nil {
		t.Fatalf("creating subject: %v", err)
	}

	if _, err := fx.repo.RetireSubject(ctx, RetireRequest{Actor: admin, ID: original.ID}); err != nil {
		t.Fatalf("retiring subject: %v", err)
	}

	// A second Subject cannot take the retired code, and the conflict names the
	// retired Subject that holds it rather than failing opaquely.
	_, err = fx.repo.CreateSubject(ctx, CreateSubjectRequest{
		Actor: admin, InstitutionID: fx.institution,
		OfficialCode: strptr("0418320"),
		TitleAr:      "مادة جديدة", TitleEn: "Reuse After Retire",
	})
	if err == nil {
		t.Fatalf("a retired Subject's official code must stay reserved")
	}
	var duplicate *DuplicateSubjectError
	if !errors.As(err, &duplicate) {
		t.Fatalf("got %v, want a DuplicateSubjectError naming the existing Subject", err)
	}
	if duplicate.Existing == nil || duplicate.Existing.ID != original.ID {
		t.Fatalf("conflict must identify the retired holder of the code, got %+v", duplicate.Existing)
	}
	if duplicate.Existing.RetiredAt == nil {
		t.Fatalf("the reported conflict should show that the holder is retired")
	}

	// The reservation cannot be released by editing the retired Subject either.
	if _, err := fx.repo.UpdateSubject(ctx, UpdateSubjectRequest{
		Actor: admin, SubjectID: original.ID, ClearCode: true,
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("clearing a retired Subject's code: got %v, want ErrSubjectCodeImmutable", err)
	}
	if _, err := fx.repo.UpdateSubject(ctx, UpdateSubjectRequest{
		Actor: admin, SubjectID: original.ID, OfficialCode: strptr("0418-999"),
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("changing a retired Subject's code: got %v, want ErrSubjectCodeImmutable", err)
	}

	// A LIVE Subject's code is equally immutable under the amended D-093 §7:
	// identity is established by the code itself, not by retirement. Reformatting
	// it remains possible, which is proven in
	// subject_code_identity_integration_test.go.
	live, err := fx.repo.CreateSubject(ctx, CreateSubjectRequest{
		Actor: admin, InstitutionID: fx.institution,
		OfficialCode: strptr("0418-400"),
		TitleAr:      "مادة حية", TitleEn: "Live Subject",
	})
	if err != nil {
		t.Fatalf("creating live subject: %v", err)
	}
	if _, err := fx.repo.UpdateSubject(ctx, UpdateSubjectRequest{
		Actor: admin, SubjectID: live.ID, OfficialCode: strptr("0418-401"),
	}); !errors.Is(err, ErrSubjectCodeImmutable) {
		t.Fatalf("renumbering a live Subject: got %v, want ErrSubjectCodeImmutable", err)
	}

	// And a retired Subject's TITLES stay editable, because only the code is
	// reserved. Nothing about this decision freezes descriptive prose.
	if _, err := fx.repo.UpdateSubject(ctx, UpdateSubjectRequest{
		Actor: admin, SubjectID: original.ID, TitleEn: strptr("Principles of Computer Systems (retired)"),
	}); err != nil {
		t.Fatalf("a retired Subject's title must stay editable: %v", err)
	}
}

func strptr(s string) *string { return &s }
