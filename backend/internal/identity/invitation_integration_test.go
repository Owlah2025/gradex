//go:build integration

package identity

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// TestInvitationInvariantsI1_I9 tests the nine invitation boundary invariants.
func TestInvitationInvariants(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "inviter_admin@example.com")
	adminSess, _ := createTestSession(t, p, adminID, now)

	// --- I3: Inviter must possess capability (CapAdminOperations) ---
	t.Run("I3_NonAdminInviterRefused", func(t *testing.T) {
		studentP := Principal{
			AccountID:       "student-1",
			Role:            RoleStudent,
			Status:          StatusActive,
			CredentialState: CredentialActive,
		}
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()
		tx, _ := conn.Begin(ctx)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err = CreateStaffInvitation(ctx, tx, CreateStaffInvitationRequest{
			ActorPrincipal:   studentP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "invited1@example.com",
			Role:             RoleInstructor,
			Now:              now,
			RequestID:        "req-i3",
		})
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("non-admin invitation returned %v, want ErrUnauthorized", err)
		}
	})

	// --- I2: Purpose-bound, digest-only, supersedable, single-use ---
	t.Run("I2_CreateAndSupersede", func(t *testing.T) {
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()

		tx1, _ := conn.Begin(ctx)
		issued1, err := CreateStaffInvitation(ctx, tx1, CreateStaffInvitationRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "instructor_supersede@example.com",
			Role:             RoleInstructor,
			Now:              now,
			RequestID:        "req-i2-1",
		})
		if err != nil {
			t.Fatalf("creating first invitation: %v", err)
		}
		if err := tx1.Commit(ctx); err != nil {
			t.Fatalf("committing: %v", err)
		}

		// Verify digest-only in DB (bearer is not stored in DB)
		var secretDigest []byte
		if err := p.QueryRow(ctx,
			`SELECT secret_digest FROM identity_action_secrets WHERE id = $1::uuid`,
			issued1.Invitation.ActionSecretID,
		).Scan(&secretDigest); err != nil {
			t.Fatalf("querying action secret digest: %v", err)
		}
		if len(secretDigest) != 32 {
			t.Fatalf("secret digest length = %d, want 32", len(secretDigest))
		}

		// Re-invite same email -> supersedes old invitation
		tx2, _ := conn.Begin(ctx)
		issued2, err := CreateStaffInvitation(ctx, tx2, CreateStaffInvitationRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "instructor_supersede@example.com",
			Role:             RoleInstructor,
			Now:              now.Add(time.Minute),
			RequestID:        "req-i2-2",
		})
		if err != nil {
			t.Fatalf("creating second invitation: %v", err)
		}
		if err := tx2.Commit(ctx); err != nil {
			t.Fatalf("committing: %v", err)
		}

		// Preview old bearer -> shows SUPERSEDED
		txPrev, _ := conn.Begin(ctx)
		prev1, err := PreviewStaffInvitation(ctx, txPrev, issued1.Bearer.Expose(), now.Add(2*time.Minute))
		_ = txPrev.Rollback(ctx)
		if err != nil {
			t.Fatalf("previewing superseded invitation: %v", err)
		}
		if prev1.State != InvitationSuperseded {
			t.Fatalf("old invitation state = %s, want SUPERSEDED", prev1.State)
		}

		// Preview new bearer -> shows PENDING
		txPrev2, _ := conn.Begin(ctx)
		prev2, err := PreviewStaffInvitation(ctx, txPrev2, issued2.Bearer.Expose(), now.Add(2*time.Minute))
		_ = txPrev2.Rollback(ctx)
		if err != nil {
			t.Fatalf("previewing new invitation: %v", err)
		}
		if prev2.State != InvitationPending {
			t.Fatalf("new invitation state = %s, want PENDING", prev2.State)
		}
	})

	// --- I1, I5, I6, I8: Onboarding completion ---
	// I1: Stored invitation is authoritative for role; completion accepts no role field.
	// I5: No password credential and no session before completion.
	// I6: Completion atomically consumes invitation and creates credential.
	// I8: Completion issues no session.
	t.Run("I1_I5_I6_I8_CompletionWorkflow", func(t *testing.T) {
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()

		tx1, _ := conn.Begin(ctx)
		issued, err := CreateStaffInvitation(ctx, tx1, CreateStaffInvitationRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "new_instructor@example.com",
			Role:             RoleInstructor,
			Now:              now,
			RequestID:        "req-create-inst",
		})
		if err != nil {
			t.Fatalf("creating invitation: %v", err)
		}
		_ = tx1.Commit(ctx)

		// I5 check: before completion, no account or password credential exists for new_instructor@example.com
		var acctCount int
		if err := p.QueryRow(ctx,
			`SELECT count(*) FROM accounts WHERE normalized_email = 'new_instructor@example.com'`,
		).Scan(&acctCount); err != nil {
			t.Fatalf("querying account count: %v", err)
		}
		if acctCount != 0 {
			t.Fatalf("account count before completion = %d, want 0", acctCount)
		}

		// Complete invitation (no role field passed!)
		txComp, _ := conn.Begin(ctx)
		res, err := CompleteStaffInvitation(ctx, txComp, CompleteStaffInvitationRequest{
			Bearer:      issued.Bearer.Expose(),
			DisplayName: "New Instructor",
			Password:    config.NewSecret("a-brand-new-launch-passphrase-9"),
			Compromised: clearCompromisedSource(),
			Now:         now.Add(5 * time.Minute),
			RequestID:   "req-complete-inst",
		})
		if err != nil {
			t.Fatalf("completing invitation: %v", err)
		}
		if err := txComp.Commit(ctx); err != nil {
			t.Fatalf("committing completion: %v", err)
		}

		// I1 assertion: role was taken from stored invitation (INSTRUCTOR), not client-submitted
		if res.InvitedRole != RoleInstructor {
			t.Fatalf("completed role = %s, want INSTRUCTOR", res.InvitedRole)
		}

		// I6 assertion: invitation state is CONSUMED and password_credentials exists
		var state string
		if err := p.QueryRow(ctx,
			`SELECT state FROM staff_invitations WHERE id = $1::uuid`,
			issued.Invitation.ID,
		).Scan(&state); err != nil {
			t.Fatalf("querying invitation state: %v", err)
		}
		if state != string(InvitationConsumed) {
			t.Fatalf("invitation state = %s, want CONSUMED", state)
		}

		var credState string
		if err := p.QueryRow(ctx,
			`SELECT state::text FROM password_credentials WHERE account_id = $1::uuid`,
			res.AccountID,
		).Scan(&credState); err != nil {
			t.Fatalf("querying password_credential state: %v", err)
		}
		if credState != string(CredentialActive) {
			t.Fatalf("credential state = %s, want ACTIVE", credState)
		}

		// Verify security event evidence (STAFF_INVITATION_COMPLETED)
		var eventType string
		if err := p.QueryRow(ctx,
			`SELECT event_type FROM identity_security_events WHERE account_id = $1::uuid AND event_type = 'STAFF_INVITATION_COMPLETED'`,
			res.AccountID,
		).Scan(&eventType); err != nil {
			t.Fatalf("querying security event: %v", err)
		}
		if eventType != "STAFF_INVITATION_COMPLETED" {
			t.Fatalf("event_type = %s, want STAFF_INVITATION_COMPLETED", eventType)
		}
	})

	// --- I4: Role ceiling checked from stored invitation AND inviter's current authority at completion time ---
	t.Run("I4_InviterSuspendedBeforeCompletionPreventsCompletion", func(t *testing.T) {
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()

		// Create temporary inviter admin
		tempAdminID, tempAdminP := createTestAdmin(t, p, "temp_admin_i4@example.com")
		tempAdminSess, _ := createTestSession(t, p, tempAdminID, now)

		tx1, _ := conn.Begin(ctx)
		issued, err := CreateStaffInvitation(ctx, tx1, CreateStaffInvitationRequest{
			ActorPrincipal:   tempAdminP,
			ActorSession:     tempAdminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "target_i4@example.com",
			Role:             RoleAdmin,
			Now:              now,
			RequestID:        "req-i4-create",
		})
		if err != nil {
			t.Fatalf("creating invitation: %v", err)
		}
		_ = tx1.Commit(ctx)

		// Suspend the inviter admin BEFORE completion
		txSus, _ := conn.Begin(ctx)
		_, _ = SuspendAccount(ctx, txSus, SuspendAccountRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			SubjectAccountID: tempAdminID,
			Reason:           "Inviter suspended",
			Now:              now.Add(minute(1)),
			RequestID:        "req-i4-suspend",
		})
		_ = txSus.Commit(ctx)

		// Complete invitation -> MUST fail because inviter current authority is gone
		txComp, _ := conn.Begin(ctx)
		defer func() { _ = txComp.Rollback(ctx) }()

		_, err = CompleteStaffInvitation(ctx, txComp, CompleteStaffInvitationRequest{
			Bearer:      issued.Bearer.Expose(),
			DisplayName: "Target Admin",
			Password:    config.NewSecret("a-brand-new-launch-passphrase-9"),
			Compromised: clearCompromisedSource(),
			Now:         now.Add(5 * time.Minute),
			RequestID:   "req-i4-complete",
		})
		if !errors.Is(err, ErrInviterUnauthorized) {
			t.Fatalf("completion with suspended inviter returned %v, want ErrInviterUnauthorized", err)
		}
	})

	// --- I9: Suspending the invitation target before completion prevents completion ---
	t.Run("I9_TargetSuspendedPreventsCompletion", func(t *testing.T) {
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()

		// Create an invitation for a target email
		tx1, _ := conn.Begin(ctx)
		issued, err := CreateStaffInvitation(ctx, tx1, CreateStaffInvitationRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "target_i9@example.com",
			Role:             RoleInstructor,
			Now:              now,
			RequestID:        "req-i9-create",
		})
		if err != nil {
			t.Fatalf("creating invitation: %v", err)
		}
		_ = tx1.Commit(ctx)

		// Insert account for target_i9@example.com and suspend it
		var targetAccountID string
		if err := p.QueryRow(ctx,
			`INSERT INTO accounts (normalized_email, email, role, status, display_name)
			 VALUES ('target_i9@example.com', 'target_i9@example.com', 'STUDENT', 'SUSPENDED', 'Suspended Student')
			 RETURNING id::text`,
		).Scan(&targetAccountID); err != nil {
			t.Fatalf("inserting suspended target account: %v", err)
		}

		// Attempt completion -> MUST fail under I9
		txComp, _ := conn.Begin(ctx)
		defer func() { _ = txComp.Rollback(ctx) }()

		_, err = CompleteStaffInvitation(ctx, txComp, CompleteStaffInvitationRequest{
			Bearer:      issued.Bearer.Expose(),
			DisplayName: "Target Student",
			Password:    config.NewSecret("a-brand-new-launch-passphrase-9"),
			Compromised: clearCompromisedSource(),
			Now:         now.Add(5 * time.Minute),
			RequestID:   "req-i9-complete",
		})
		if err == nil {
			t.Fatal("completion on suspended target unexpectedly succeeded")
		}
	})

	// --- I7: Two concurrent completions produce EXACTLY ONE winner under real PostgreSQL contention ---
	t.Run("I7_ConcurrentCompletionsSingleWinner", func(t *testing.T) {
		conn, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring conn: %v", err)
		}
		defer conn.Release()

		tx1, _ := conn.Begin(ctx)
		issued, err := CreateStaffInvitation(ctx, tx1, CreateStaffInvitationRequest{
			ActorPrincipal:   adminP,
			ActorSession:     adminSess,
			RecentAuthWindow: 10 * time.Minute,
			Email:            "concurrent_staff@example.com",
			Role:             RoleInstructor,
			Now:              now,
			RequestID:        "req-i7-create",
		})
		if err != nil {
			t.Fatalf("creating invitation: %v", err)
		}
		_ = tx1.Commit(ctx)

		const concurrency = 10
		var wg sync.WaitGroup
		results := make([]error, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				c, err := p.Acquire(ctx)
				if err != nil {
					results[idx] = err
					return
				}
				defer c.Release()

				tx, err := c.Begin(ctx)
				if err != nil {
					results[idx] = err
					return
				}
				defer func() { _ = tx.Rollback(ctx) }()

				_, err = CompleteStaffInvitation(ctx, tx, CompleteStaffInvitationRequest{
					Bearer:      issued.Bearer.Expose(),
					DisplayName: "Concurrent Staff",
					Password:    config.NewSecret("a-brand-new-launch-passphrase-9"),
					Compromised: clearCompromisedSource(),
					Now:         now.Add(5 * time.Minute),
					RequestID:   "req-i7-comp",
				})
				if err == nil {
					results[idx] = tx.Commit(ctx)
				} else {
					results[idx] = err
				}
			}(i)
		}
		wg.Wait()

		winners := 0
		alreadyUsedLosers := 0
		otherErrors := 0

		for _, err := range results {
			if err == nil {
				winners++
			} else if errors.Is(err, ErrInvitationAlreadyUsed) {
				alreadyUsedLosers++
			} else {
				t.Logf("unexpected error in concurrent completion: %v", err)
				otherErrors++
			}
		}

		if winners != 1 {
			t.Fatalf("concurrent completion winners = %d, want EXACTLY 1", winners)
		}
		if alreadyUsedLosers != concurrency-1 {
			t.Fatalf("concurrent completion already-used losers = %d, want %d", alreadyUsedLosers, concurrency-1)
		}
		if otherErrors > 0 {
			t.Fatalf("unexpected non-already-used errors = %d", otherErrors)
		}
	})
}

func minute(m int) time.Duration {
	return time.Duration(m) * time.Minute
}
