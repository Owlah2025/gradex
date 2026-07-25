//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

const recentAuthWindow = 15 * time.Minute

var adminPasswordChangePolicy = PasswordChangePolicy{
	RecentAuthWindow: recentAuthWindow,
	IdleExpiry:       30 * time.Minute,
	AbsoluteExpiry:   12 * time.Hour,
}

// bootstrapWithSession creates the bootstrap Admin and an authenticated session
// family at generation 1, which is the state link 5 will complete from.
func bootstrapWithSession(t *testing.T, conn *pgx.Conn, ctx context.Context) (accountID, sessionID string) {
	t.Helper()

	result, err := Bootstrap(ctx, conn, request("op-pwchange"))
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}
	accountID = result.AccountID

	issued, err := NewSessionCredential()
	if err != nil {
		t.Fatalf("minting a session credential: %v", err)
	}

	now := time.Now().UTC()
	if err := conn.QueryRow(ctx,
		`INSERT INTO sessions (account_id, admitted_epoch, authenticated_at, last_activity_at,
		                       idle_expires_at, absolute_expires_at)
		 VALUES ($1::uuid, 1, $2, $2, $3, $4)
		 RETURNING id::text`,
		accountID, now, now.Add(12*time.Hour), now.Add(720*time.Hour),
	).Scan(&sessionID); err != nil {
		t.Fatalf("creating the session: %v", err)
	}

	if _, err := conn.Exec(ctx,
		`INSERT INTO session_credentials (session_id, generation, credential_digest, csrf_digest)
		 VALUES ($1::uuid, 1, $2, $3)`,
		sessionID, issued.CredentialDigest, issued.CSRFDigest,
	); err != nil {
		t.Fatalf("creating the session credential: %v", err)
	}
	return accountID, sessionID
}

func inTx(t *testing.T, conn *pgx.Conn, ctx context.Context, fn func(pgx.Tx)) {
	t.Helper()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	fn(tx)
}

// assertNothingMutated is the invariant every failure path must satisfy: the old
// password, the restriction state, and the session all remain authoritative.
func assertNothingMutated(t *testing.T, conn *pgx.Conn, ctx context.Context, accountID, sessionID string) {
	t.Helper()

	var credentialState, storedHash string
	if err := conn.QueryRow(ctx,
		`SELECT state::text, password_hash FROM password_credentials WHERE account_id = $1::uuid`,
		accountID,
	).Scan(&credentialState, &storedHash); err != nil {
		t.Fatalf("reading credential: %v", err)
	}
	if credentialState != "CHANGE_REQUIRED" {
		t.Errorf("credential state = %q, want CHANGE_REQUIRED to be untouched", credentialState)
	}
	// The original bootstrap password must still be the one that works.
	ok, err := VerifyPassword(storedHash, "correct-horse-battery-staple-7")
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !ok {
		t.Error("the original password no longer verifies; a failed change mutated the credential")
	}

	var generation int
	var state string
	if err := conn.QueryRow(ctx,
		`SELECT s.current_generation, c.state::text
		   FROM sessions s JOIN session_credentials c
		     ON c.session_id = s.id AND c.generation = s.current_generation
		  WHERE s.id = $1::uuid`,
		sessionID,
	).Scan(&generation, &state); err != nil {
		t.Fatalf("reading session: %v", err)
	}
	if generation != 1 || state != "CURRENT" {
		t.Errorf("session moved to generation %d/%s; a failed change rotated it", generation, state)
	}
}

// Preparation performs no mutation even on the success path. Everything that
// changes state is link 5.
func TestPrepareSucceedsWithoutMutatingAnything(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		prepared, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           accountID,
			SessionID:           sessionID,
			PresentedGeneration: 1,
			Kind:                BootstrapMandatoryChange,
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())
		if err != nil {
			t.Fatalf("preparing: %v", err)
		}

		if prepared.NewPasswordHash.IsEmpty() {
			t.Fatal("preparation produced no hash")
		}
		if prepared.CurrentGeneration != 1 {
			t.Errorf("generation = %d, want 1", prepared.CurrentGeneration)
		}
		// The bootstrap Account's first session has no others to revoke.
		if prepared.RevokeOtherSessions {
			t.Error("the bootstrap mandatory change asked to revoke other sessions")
		}
	})

	assertNothingMutated(t, conn, ctx, accountID, sessionID)
}

// Bootstrap close condition 4: the password, restriction state, session
// credential, CSRF token, expiry windows, and Audit evidence become one
// committed fact.
func TestCompletePasswordChangeAtomicallyClearsRestrictionAndRotatesSession(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	result, err := CompletePasswordChange(ctx, conn, PasswordChangeRequest{
		AccountID:           accountID,
		SessionID:           sessionID,
		PresentedGeneration: 1,
		Kind:                BootstrapMandatoryChange,
		NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
	}, adminPasswordChangePolicy, now)
	if err != nil {
		t.Fatalf("completing password change: %v", err)
	}

	if result.SessionID != sessionID || result.Generation != 2 {
		t.Fatalf("replacement = session %q generation %d, want %q generation 2",
			result.SessionID, result.Generation, sessionID)
	}
	if result.Credential.IsEmpty() || result.CSRFToken.IsEmpty() {
		t.Fatal("successful completion did not return both replacement browser credentials")
	}
	if result.Credential.Expose() == result.CSRFToken.Expose() {
		t.Fatal("the replacement session and CSRF credentials are identical")
	}
	if !result.IdleExpiresAt.Equal(now.Add(30 * time.Minute)) {
		t.Errorf("idle expiry = %s, want %s", result.IdleExpiresAt, now.Add(30*time.Minute))
	}
	if !result.AbsoluteExpiresAt.Equal(now.Add(12 * time.Hour)) {
		t.Errorf("absolute expiry = %s, want %s", result.AbsoluteExpiresAt, now.Add(12*time.Hour))
	}

	var credentialState, storedHash string
	if err := conn.QueryRow(ctx,
		`SELECT state::text, password_hash
		   FROM password_credentials WHERE account_id = $1::uuid`,
		accountID,
	).Scan(&credentialState, &storedHash); err != nil {
		t.Fatalf("reading replaced password: %v", err)
	}
	if credentialState != "ACTIVE" {
		t.Errorf("credential state = %q, want ACTIVE", credentialState)
	}
	ok, err := VerifyPassword(storedHash, "a-brand-new-launch-passphrase-9")
	if err != nil {
		t.Fatalf("verifying replacement password: %v", err)
	}
	if !ok {
		t.Error("the replacement password does not verify")
	}
	oldOK, err := VerifyPassword(storedHash, "correct-horse-battery-staple-7")
	if err != nil {
		t.Fatalf("checking superseded password: %v", err)
	}
	if oldOK {
		t.Error("the temporary bootstrap password still verifies")
	}

	var (
		currentGeneration                  int
		authenticatedAt, reauthenticatedAt time.Time
		idleExpiresAt, absoluteExpiresAt   time.Time
		oldState                           string
		oldReplacedBy                      int
		oldSupersededAt                    time.Time
		newState, newCredential, newCSRF   string
	)
	if err := conn.QueryRow(ctx,
		`SELECT current_generation, authenticated_at, reauthenticated_at,
		        idle_expires_at, absolute_expires_at
		   FROM sessions WHERE id = $1::uuid`,
		sessionID,
	).Scan(&currentGeneration, &authenticatedAt, &reauthenticatedAt,
		&idleExpiresAt, &absoluteExpiresAt); err != nil {
		t.Fatalf("reading re-established session: %v", err)
	}
	if currentGeneration != 2 {
		t.Errorf("current generation = %d, want 2", currentGeneration)
	}
	for name, times := range map[string]struct {
		got  time.Time
		want time.Time
	}{
		"authenticated":   {authenticatedAt, now},
		"reauthenticated": {reauthenticatedAt, now},
		"idle expiry":     {idleExpiresAt, result.IdleExpiresAt},
		"absolute expiry": {absoluteExpiresAt, result.AbsoluteExpiresAt},
	} {
		if !times.got.Equal(times.want) {
			t.Errorf("%s at = %s, want %s", name, times.got, times.want)
		}
	}

	if err := conn.QueryRow(ctx,
		`SELECT state::text, replaced_by_generation, superseded_at
		   FROM session_credentials
		  WHERE session_id = $1::uuid AND generation = 1`,
		sessionID,
	).Scan(&oldState, &oldReplacedBy, &oldSupersededAt); err != nil {
		t.Fatalf("reading superseded generation: %v", err)
	}
	if oldState != "SUPERSEDED" || oldReplacedBy != 2 || !oldSupersededAt.Equal(now) {
		t.Errorf("old generation = %s replaced-by %d at %s, want SUPERSEDED/2/%s",
			oldState, oldReplacedBy, oldSupersededAt, now)
	}

	if err := conn.QueryRow(ctx,
		`SELECT state::text, credential_digest, csrf_digest
		   FROM session_credentials
		  WHERE session_id = $1::uuid AND generation = 2`,
		sessionID,
	).Scan(&newState, &newCredential, &newCSRF); err != nil {
		t.Fatalf("reading replacement generation: %v", err)
	}
	if newState != "CURRENT" {
		t.Errorf("replacement state = %q, want CURRENT", newState)
	}
	if newCredential != DigestToken(result.Credential.Expose()) {
		t.Error("stored replacement credential digest does not match returned credential")
	}
	if newCSRF != DigestToken(result.CSRFToken.Expose()) {
		t.Error("stored replacement CSRF digest does not match returned token")
	}
	if newCredential == result.Credential.Expose() || newCSRF == result.CSRFToken.Expose() {
		t.Error("a plaintext replacement credential was stored")
	}

	var auditAction, auditRole, auditKind string
	var auditGeneration int
	if err := conn.QueryRow(ctx,
		`SELECT action, actor_role, metadata->>'change_kind',
		        (metadata->>'replacement_generation')::integer
		   FROM audit_events
		  WHERE target_id = $1 AND action = 'PASSWORD_CHANGED'`,
		accountID,
	).Scan(&auditAction, &auditRole, &auditKind, &auditGeneration); err != nil {
		t.Fatalf("reading password-change Audit evidence: %v", err)
	}
	if auditAction != "PASSWORD_CHANGED" || auditRole != "ADMIN" ||
		auditKind != "BOOTSTRAP_MANDATORY" || auditGeneration != 2 {
		t.Errorf("Audit = %s/%s/%s/%d, want PASSWORD_CHANGED/ADMIN/BOOTSTRAP_MANDATORY/2",
			auditAction, auditRole, auditKind, auditGeneration)
	}
}

// If the final evidence write fails, none of the earlier statements may
// survive. This proves atomicity at the farthest failure point in the command.
func TestCompletePasswordChangeRollsBackWhenAuditCannotCommit(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	if _, err := conn.Exec(ctx, `
		CREATE FUNCTION reject_password_change_audit() RETURNS TRIGGER AS $$
		BEGIN
		    IF NEW.action = 'PASSWORD_CHANGED' THEN
		        RAISE EXCEPTION 'injected Audit failure';
		    END IF;
		    RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_password_change_audit
		    BEFORE INSERT ON audit_events
		    FOR EACH ROW EXECUTE FUNCTION reject_password_change_audit();
	`); err != nil {
		t.Fatalf("installing Audit failure injection: %v", err)
	}

	_, err := CompletePasswordChange(ctx, conn, PasswordChangeRequest{
		AccountID:           accountID,
		SessionID:           sessionID,
		PresentedGeneration: 1,
		Kind:                BootstrapMandatoryChange,
		NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
	}, adminPasswordChangePolicy, time.Now().UTC())
	if err == nil {
		t.Fatal("password change succeeded despite the required Audit write failing")
	}

	assertNothingMutated(t, conn, ctx, accountID, sessionID)

	var passwordChangeAudits int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE action = 'PASSWORD_CHANGED'`,
	).Scan(&passwordChangeAudits); err != nil {
		t.Fatalf("counting password-change Audit rows: %v", err)
	}
	if passwordChangeAudits != 0 {
		t.Fatalf("failed change left %d password-change Audit rows", passwordChangeAudits)
	}
}

func TestVoluntaryPasswordChangeRevokesEveryOtherSessionFamily(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	var otherSessionID string
	now := time.Now().UTC()
	if err := conn.QueryRow(ctx,
		`INSERT INTO sessions
		   (account_id, admitted_epoch, authenticated_at, last_activity_at,
		    idle_expires_at, absolute_expires_at)
		 VALUES ($1::uuid, 1, $2, $2, $3, $4)
		 RETURNING id::text`,
		accountID, now, now.Add(time.Hour), now.Add(12*time.Hour),
	).Scan(&otherSessionID); err != nil {
		t.Fatalf("creating other session: %v", err)
	}
	otherCredential, err := NewSessionCredential()
	if err != nil {
		t.Fatalf("minting other credential: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO session_credentials
		   (session_id, generation, credential_digest, csrf_digest)
		 VALUES ($1::uuid, 1, $2, $3)`,
		otherSessionID, otherCredential.CredentialDigest, otherCredential.CSRFDigest,
	); err != nil {
		t.Fatalf("inserting other credential: %v", err)
	}

	result, err := CompletePasswordChange(ctx, conn, PasswordChangeRequest{
		AccountID:           accountID,
		SessionID:           sessionID,
		PresentedGeneration: 1,
		Kind:                VoluntaryChange,
		CurrentPassword:     config.NewSecret("correct-horse-battery-staple-7"),
		NewPassword:         config.NewSecret("a-voluntary-launch-passphrase-8"),
	}, adminPasswordChangePolicy, now)
	if err != nil {
		t.Fatalf("completing voluntary change: %v", err)
	}
	if result.Generation != 2 {
		t.Errorf("replacement generation = %d, want 2", result.Generation)
	}

	var currentState, otherState, otherReason string
	if err := conn.QueryRow(ctx,
		`SELECT state::text FROM sessions WHERE id = $1::uuid`,
		sessionID,
	).Scan(&currentState); err != nil {
		t.Fatalf("reading current session: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`SELECT state::text, revocation_reason::text
		   FROM sessions WHERE id = $1::uuid`,
		otherSessionID,
	).Scan(&otherState, &otherReason); err != nil {
		t.Fatalf("reading other session: %v", err)
	}
	if currentState != "ACTIVE" {
		t.Errorf("current family state = %q, want ACTIVE", currentState)
	}
	if otherState != "REVOKED" || otherReason != "PASSWORD_CHANGE" {
		t.Errorf("other family = %s/%s, want REVOKED/PASSWORD_CHANGE", otherState, otherReason)
	}
}

func TestWrongCurrentPasswordIsRefusedAndChangesNothing(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           accountID,
			SessionID:           sessionID,
			PresentedGeneration: 1,
			Kind:                VoluntaryChange,
			CurrentPassword:     config.NewSecret("not-the-current-password-at-all"),
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())

		if !errors.Is(err, ErrCurrentPasswordIncorrect) {
			t.Fatalf("err = %v, want ErrCurrentPasswordIncorrect", err)
		}
	})

	assertNothingMutated(t, conn, ctx, accountID, sessionID)
}

func TestMissingCurrentPasswordIsRefusedForVoluntaryChange(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           accountID,
			SessionID:           sessionID,
			PresentedGeneration: 1,
			Kind:                VoluntaryChange,
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())

		if !errors.Is(err, ErrCurrentPasswordRequired) {
			t.Fatalf("err = %v, want ErrCurrentPasswordRequired", err)
		}
	})

	assertNothingMutated(t, conn, ctx, accountID, sessionID)
}

// A stale generation means the session rotated underneath this request.
// Committing against it would let a superseded credential authorize a change.
func TestStaleGenerationCannotPrepareAChange(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	t.Run("presented generation behind the session", func(t *testing.T) {
		// Move the family to generation 2, as a rotation would.
		issued, err := NewSessionCredential()
		if err != nil {
			t.Fatalf("minting: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`UPDATE session_credentials SET state = 'SUPERSEDED', superseded_at = now(),
			        replaced_by_generation = 2
			  WHERE session_id = $1::uuid AND generation = 1`, sessionID); err != nil {
			t.Fatalf("superseding: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`INSERT INTO session_credentials (session_id, generation, credential_digest, csrf_digest)
			 VALUES ($1::uuid, 2, $2, $3)`, sessionID, issued.CredentialDigest, issued.CSRFDigest); err != nil {
			t.Fatalf("inserting generation 2: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`UPDATE sessions SET current_generation = 2 WHERE id = $1::uuid`, sessionID); err != nil {
			t.Fatalf("advancing: %v", err)
		}

		inTx(t, conn, ctx, func(tx pgx.Tx) {
			_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
				AccountID:           accountID,
				SessionID:           sessionID,
				PresentedGeneration: 1, // the superseded one
				Kind:                BootstrapMandatoryChange,
				NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
			}, recentAuthWindow, time.Now().UTC())

			if !errors.Is(err, ErrStaleGeneration) {
				t.Fatalf("err = %v, want ErrStaleGeneration", err)
			}
		})
	})

	t.Run("sessions row points at a superseded generation", func(t *testing.T) {
		// The pointer and the credential state disagree. Checking only the
		// number would accept this.
		if _, err := conn.Exec(ctx,
			`UPDATE session_credentials SET state = 'SUPERSEDED', superseded_at = now()
			  WHERE session_id = $1::uuid AND generation = 2`, sessionID); err != nil {
			t.Fatalf("superseding generation 2: %v", err)
		}

		inTx(t, conn, ctx, func(tx pgx.Tx) {
			_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
				AccountID:           accountID,
				SessionID:           sessionID,
				PresentedGeneration: 2,
				Kind:                BootstrapMandatoryChange,
				NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
			}, recentAuthWindow, time.Now().UTC())

			if !errors.Is(err, ErrStaleGeneration) {
				t.Fatalf("err = %v, want ErrStaleGeneration", err)
			}
		})
	})
}

func TestInsufficientRecentAuthenticationIsRefused(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	// Authenticated well outside the window, but still inside idle expiry, so
	// the only thing that can refuse it is the recent-auth rule.
	if _, err := conn.Exec(ctx,
		`UPDATE sessions SET authenticated_at = now() - interval '2 hours' WHERE id = $1::uuid`,
		sessionID); err != nil {
		t.Fatalf("ageing the session: %v", err)
	}

	for _, tc := range []struct {
		name            string
		kind            PasswordChangeKind
		currentPassword config.Secret
	}{
		{
			name:            "voluntary change",
			kind:            VoluntaryChange,
			currentPassword: config.NewSecret("correct-horse-battery-staple-7"),
		},
		{
			name: "bootstrap mandatory change",
			kind: BootstrapMandatoryChange,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inTx(t, conn, ctx, func(tx pgx.Tx) {
				_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
					AccountID:           accountID,
					SessionID:           sessionID,
					PresentedGeneration: 1,
					Kind:                tc.kind,
					CurrentPassword:     tc.currentPassword,
					NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
				}, recentAuthWindow, time.Now().UTC())

				if !errors.Is(err, ErrRecentAuthRequired) {
					t.Fatalf("err = %v, want ErrRecentAuthRequired", err)
				}
			})
			assertNothingMutated(t, conn, ctx, accountID, sessionID)
		})
	}
}

func TestWeakPasswordDoesNotMutateCredentialsOrSessions(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	for name, weak := range map[string]string{
		"too short":        "short",
		"common substring": "mypasswordisreallylong",
		"same as current":  "correct-horse-battery-staple-7",
	} {
		t.Run(name, func(t *testing.T) {
			inTx(t, conn, ctx, func(tx pgx.Tx) {
				_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
					AccountID:           accountID,
					SessionID:           sessionID,
					PresentedGeneration: 1,
					Kind:                BootstrapMandatoryChange,
					NewPassword:         config.NewSecret(weak),
				}, recentAuthWindow, time.Now().UTC())

				if !errors.Is(err, ErrPasswordPolicy) {
					t.Fatalf("err = %v, want ErrPasswordPolicy", err)
				}
			})
			assertNothingMutated(t, conn, ctx, accountID, sessionID)
		})
	}
}

// An Account becoming inactive between admission and this transaction must
// win, regardless of which inactive lifecycle state it entered.
func TestInactiveAccountCannotPrepareAPasswordChange(t *testing.T) {
	for _, status := range []string{"PENDING_VERIFICATION", "SUSPENDED"} {
		t.Run(status, func(t *testing.T) {
			freshSchema(t)
			conn, ctx := connect(t)
			accountID, sessionID := bootstrapWithSession(t, conn, ctx)

			if _, err := conn.Exec(ctx,
				`UPDATE accounts
				    SET status = $2::account_status,
				        email_verified_at = CASE
				            WHEN $2 = 'PENDING_VERIFICATION' THEN NULL
				            ELSE email_verified_at
				        END
				  WHERE id = $1::uuid`,
				accountID, status,
			); err != nil {
				t.Fatalf("making Account inactive: %v", err)
			}

			inTx(t, conn, ctx, func(tx pgx.Tx) {
				_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
					AccountID:           accountID,
					SessionID:           sessionID,
					PresentedGeneration: 1,
					Kind:                BootstrapMandatoryChange,
					NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
				}, recentAuthWindow, time.Now().UTC())

				if !errors.Is(err, ErrSessionNotUsable) {
					t.Fatalf("err = %v, want ErrSessionNotUsable", err)
				}
			})
		})
	}
}

// A session admitted under a superseded Account epoch is not usable, which is
// what makes logout-all and suspension immediate.
func TestStaleEpochSessionCannotPrepareAPasswordChange(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	if _, err := conn.Exec(ctx,
		`UPDATE accounts SET session_epoch = session_epoch + 1 WHERE id = $1::uuid`, accountID); err != nil {
		t.Fatalf("bumping the epoch: %v", err)
	}

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           accountID,
			SessionID:           sessionID,
			PresentedGeneration: 1,
			Kind:                BootstrapMandatoryChange,
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())

		if !errors.Is(err, ErrSessionNotUsable) {
			t.Fatalf("err = %v, want ErrSessionNotUsable", err)
		}
	})

	assertNothingMutated(t, conn, ctx, accountID, sessionID)
}

// A session belonging to a different Account must not authorize this Account's
// password change.
func TestForeignSessionCannotPrepareAChange(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	var otherID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('other@gradex.example', 'other@gradex.example', 'STUDENT', 'ACTIVE', 'Other')
		 RETURNING id::text`).Scan(&otherID); err != nil {
		t.Fatalf("creating the other Account: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO password_credentials (account_id, password_hash, state)
		 VALUES ($1::uuid, $2, 'ACTIVE')`, otherID,
		"$argon2id$v=19$m=65536,t=3,p=4$c21va2VzYWx0c21va2Vz$bm90LWEtcmVhbC1jcmVkZW50aWFsLXNlZWQwMQ",
	); err != nil {
		t.Fatalf("creating the other credential: %v", err)
	}

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           otherID,
			SessionID:           sessionID, // belongs to the bootstrap Admin
			PresentedGeneration: 1,
			Kind:                VoluntaryChange,
			CurrentPassword:     config.NewSecret("correct-horse-battery-staple-7"),
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())

		if !errors.Is(err, ErrSessionNotUsable) {
			t.Fatalf("err = %v, want ErrSessionNotUsable", err)
		}
	})

	assertNothingMutated(t, conn, ctx, accountID, sessionID)
}

// A caller must not be able to claim the mandatory flow on an ordinary
// credential and thereby skip proving the current password.
func TestMandatoryKindIsRefusedOnAnActiveCredential(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	accountID, sessionID := bootstrapWithSession(t, conn, ctx)

	if _, err := conn.Exec(ctx,
		`UPDATE password_credentials SET state = 'ACTIVE' WHERE account_id = $1::uuid`, accountID); err != nil {
		t.Fatalf("clearing the requirement: %v", err)
	}

	inTx(t, conn, ctx, func(tx pgx.Tx) {
		_, err := PreparePasswordChange(ctx, tx, PasswordChangeRequest{
			AccountID:           accountID,
			SessionID:           sessionID,
			PresentedGeneration: 1,
			Kind:                BootstrapMandatoryChange,
			NewPassword:         config.NewSecret("a-brand-new-launch-passphrase-9"),
		}, recentAuthWindow, time.Now().UTC())

		if !errors.Is(err, ErrCurrentPasswordRequired) {
			t.Fatalf("err = %v, want ErrCurrentPasswordRequired", err)
		}
	})
}

// The immutability trigger is a database guarantee, not a convention.
func TestSessionCredentialIdentityIsImmutable(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	_, sessionID := bootstrapWithSession(t, conn, ctx)

	for name, stmt := range map[string]string{
		"credential digest": `UPDATE session_credentials SET credential_digest = 'rewritten' WHERE session_id = $1::uuid`,
		"csrf digest":       `UPDATE session_credentials SET csrf_digest = 'rewritten' WHERE session_id = $1::uuid`,
		"generation":        `UPDATE session_credentials SET generation = 99 WHERE session_id = $1::uuid`,
		"issued_at":         `UPDATE session_credentials SET issued_at = now() - interval '1 day' WHERE session_id = $1::uuid`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := conn.Exec(ctx, stmt, sessionID); err == nil {
				t.Fatalf("%s was rewritten; the generation is not immutable", name)
			}
		})
	}
}

// Two CURRENT generations in one family would mean a rotation that did not
// supersede its predecessor.
func TestOnlyOneCurrentGenerationPerSession(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	_, sessionID := bootstrapWithSession(t, conn, ctx)

	issued, err := NewSessionCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO session_credentials (session_id, generation, credential_digest, csrf_digest)
		 VALUES ($1::uuid, 2, $2, $3)`,
		sessionID, issued.CredentialDigest, issued.CSRFDigest,
	); err == nil {
		t.Fatal("a second CURRENT generation was accepted")
	}
}

// A superseded generation must never return to CURRENT.
func TestSupersededGenerationCannotBecomeCurrentAgain(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	_, sessionID := bootstrapWithSession(t, conn, ctx)

	if _, err := conn.Exec(ctx,
		`UPDATE session_credentials SET state = 'SUPERSEDED', superseded_at = now()
		  WHERE session_id = $1::uuid AND generation = 1`, sessionID); err != nil {
		t.Fatalf("superseding: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`UPDATE session_credentials SET state = 'CURRENT', superseded_at = NULL
		  WHERE session_id = $1::uuid AND generation = 1`, sessionID); err == nil {
		t.Fatal("a superseded generation was restored to CURRENT")
	}
}

// A revoked family must carry both its time and its reason.
func TestRevocationEvidenceMustBeComplete(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)
	_, sessionID := bootstrapWithSession(t, conn, ctx)

	if _, err := conn.Exec(ctx,
		`UPDATE sessions SET state = 'REVOKED' WHERE id = $1::uuid`, sessionID); err == nil {
		t.Fatal("a family was revoked with no time or reason")
	}

	if _, err := conn.Exec(ctx,
		`UPDATE sessions SET state = 'REVOKED', revoked_at = now(), revocation_reason = 'PASSWORD_CHANGE'
		  WHERE id = $1::uuid`, sessionID); err != nil {
		t.Fatalf("a complete revocation was refused: %v", err)
	}
}
