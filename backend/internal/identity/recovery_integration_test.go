//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func recoveryService(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
	randomByte byte,
) *RecoveryService {
	t.Helper()
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	randomness := make([]byte, 0, 32*8)
	for offset := byte(0); offset < 8; offset++ {
		randomness = append(randomness, bytes.Repeat([]byte{randomByte + offset}, 32)...)
	}
	service, err := NewRecoveryService(RecoveryServiceOptions{
		Pool:        pool,
		Outbox:      writer,
		Compromised: clearCompromisedSource(),
		ResetTTL:    time.Hour,
		Now:         func() time.Time { return now },
		Random:      bytes.NewReader(randomness),
	})
	if err != nil {
		t.Fatalf("constructing recovery service: %v", err)
	}
	return service
}

// activeStudent registers and verifies a Student so recovery has an eligible
// Account to act on.
func activeStudent(t *testing.T, pool *pgxpool.Pool, verificationByte byte) {
	t.Helper()
	admission := admissionService(t, pool, time.Now().UTC(), verificationByte)
	if err := admission.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := admission.VerifyEmail(
		context.Background(), deterministicBearer(verificationByte), "request-verify-1",
	); err != nil {
		t.Fatalf("verifying: %v", err)
	}
}

func countLiveResetSecrets(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_action_secrets
		  WHERE purpose = 'PASSWORD_RESET'
		    AND consumed_at IS NULL AND superseded_at IS NULL`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("counting live reset secrets: %v", err)
	}
	return count
}

// attemptConsumption runs the lock-then-consume pair the real recovery
// transaction uses. It is exercised through the unexported primitives on
// purpose: exporting a standalone "consume this reset secret" operation would
// create a supported way to burn a reset secret without replacing a password,
// which is precisely the state that strands an Account.
func attemptConsumption(pool *pgxpool.Pool, digest []byte, now time.Time) (bool, error) {
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	secretID, _, valid, err := lockResetSecret(ctx, tx, digest, now)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, tx.Commit(ctx)
	}
	if err := consumeResetSecret(ctx, tx, secretID, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// TestPasswordResetRequestIsUniformAcrossAccountStates is the non-enumeration
// proof. Every state returns the same nil result over the same floor, and only
// the eligible Account gains a secret.
func TestPasswordResetRequestIsUniformAcrossAccountStates(t *testing.T) {
	tests := []struct {
		scenario   string
		email      string
		setup      func(t *testing.T, pool *pgxpool.Pool)
		wantSecret int
	}{
		{
			scenario:   "unknown address",
			email:      "absent.person@example.com",
			setup:      func(*testing.T, *pgxpool.Pool) {},
			wantSecret: 0,
		},
		{
			scenario: "registered but unverified",
			email:    studentRegistration().Email,
			setup: func(t *testing.T, pool *pgxpool.Pool) {
				admission := admissionService(t, pool, time.Now().UTC(), 0x61)
				if err := admission.RegisterStudent(
					context.Background(), studentRegistration(),
				); err != nil {
					t.Fatalf("registering: %v", err)
				}
			},
			wantSecret: 0,
		},
		{
			scenario: "active and verified",
			email:    studentRegistration().Email,
			setup: func(t *testing.T, pool *pgxpool.Pool) {
				activeStudent(t, pool, 0x62)
			},
			wantSecret: 1,
		},
		{
			scenario:   "malformed address",
			email:      "not-an-address",
			setup:      func(*testing.T, *pgxpool.Pool) {},
			wantSecret: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			pool := admissionPool(t)
			test.setup(t, pool)
			service := recoveryService(t, pool, time.Now().UTC(), 0x70)

			started := time.Now()
			err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
				Email:     test.email,
				RequestID: "request-reset-1",
			})
			elapsed := time.Since(started)

			if err != nil {
				t.Fatalf("reset request returned %v, want nil for every Account state", err)
			}
			if elapsed < minimumPasswordResetRequestDuration {
				t.Fatalf(
					"reset request took %s, below the %s floor that hides Account existence",
					elapsed, minimumPasswordResetRequestDuration,
				)
			}
			if live := countLiveResetSecrets(t, pool); live != test.wantSecret {
				t.Fatalf("live reset secrets = %d, want %d", live, test.wantSecret)
			}
		})
	}
}

// TestConcurrentResetConsumptionHasExactlyOneWinner proves single use under
// real contention rather than under a sequential replay.
func TestConcurrentResetConsumptionHasExactlyOneWinner(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x63)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x71)
	if err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
		Email: studentRegistration().Email, RequestID: "request-reset-1",
	}); err != nil {
		t.Fatalf("requesting reset: %v", err)
	}
	digest, err := DigestActionSecret(deterministicBearer(0x71))
	if err != nil {
		t.Fatalf("digesting bearer: %v", err)
	}

	const attempts = 6
	var wait sync.WaitGroup
	var mutex sync.Mutex
	winners := 0
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			won, err := attemptConsumption(pool, digest, now)
			mutex.Lock()
			defer mutex.Unlock()
			if err != nil {
				t.Errorf("concurrent consumption: %v", err)
				return
			}
			if won {
				winners++
			}
		}()
	}
	wait.Wait()

	if winners != 1 {
		t.Fatalf("consumption winners = %d, want exactly 1", winners)
	}
	if live := countLiveResetSecrets(t, pool); live != 0 {
		t.Fatalf("live reset secrets after consumption = %d, want 0", live)
	}
}

// TestResetSecretRefusesExpiredReplayedAndWrongPurpose covers the three ways a
// presented secret must fail closed.
func TestResetSecretRefusesExpiredReplayedAndWrongPurpose(t *testing.T) {
	t.Run("replayed", func(t *testing.T) {
		pool := admissionPool(t)
		activeStudent(t, pool, 0x64)
		now := time.Now().UTC()
		service := recoveryService(t, pool, now, 0x72)
		if err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
			Email: studentRegistration().Email, RequestID: "request-reset-1",
		}); err != nil {
			t.Fatalf("requesting reset: %v", err)
		}
		digest, err := DigestActionSecret(deterministicBearer(0x72))
		if err != nil {
			t.Fatalf("digesting bearer: %v", err)
		}
		first, err := attemptConsumption(pool, digest, now)
		if err != nil || !first {
			t.Fatalf("first consumption = %v, %v; want true, nil", first, err)
		}
		second, err := attemptConsumption(pool, digest, now)
		if err != nil {
			t.Fatalf("second consumption: %v", err)
		}
		if second {
			t.Fatal("a consumed reset secret was accepted a second time")
		}
	})

	t.Run("expired", func(t *testing.T) {
		pool := admissionPool(t)
		activeStudent(t, pool, 0x65)
		issuedAt := time.Now().UTC()
		service := recoveryService(t, pool, issuedAt, 0x73)
		if err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
			Email: studentRegistration().Email, RequestID: "request-reset-1",
		}); err != nil {
			t.Fatalf("requesting reset: %v", err)
		}
		digest, err := DigestActionSecret(deterministicBearer(0x73))
		if err != nil {
			t.Fatalf("digesting bearer: %v", err)
		}
		// The service issued a one-hour secret; present it two hours later.
		consumed, err := attemptConsumption(pool, digest, issuedAt.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("expired consumption: %v", err)
		}
		if consumed {
			t.Fatal("an expired reset secret was accepted")
		}
	})

	t.Run("wrong purpose", func(t *testing.T) {
		pool := admissionPool(t)
		// Register only, leaving a live EMAIL_VERIFICATION secret and no reset
		// secret at all.
		admission := admissionService(t, pool, time.Now().UTC(), 0x66)
		if err := admission.RegisterStudent(
			context.Background(), studentRegistration(),
		); err != nil {
			t.Fatalf("registering: %v", err)
		}
		digest, err := DigestActionSecret(deterministicBearer(0x66))
		if err != nil {
			t.Fatalf("digesting bearer: %v", err)
		}
		consumed, err := attemptConsumption(pool, digest, time.Now().UTC())
		if err != nil {
			t.Fatalf("wrong-purpose consumption: %v", err)
		}
		if consumed {
			t.Fatal("an email-verification secret was accepted for password recovery")
		}
	})
}

// TestResetRequestSupersedesPreviousLiveSecret proves a reissue invalidates the
// earlier secret rather than leaving two usable paths into one Account.
func TestResetRequestSupersedesPreviousLiveSecret(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x67)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x74)
	for _, requestID := range []string{"request-reset-1", "request-reset-2"} {
		if err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
			Email: studentRegistration().Email, RequestID: requestID,
		}); err != nil {
			t.Fatalf("requesting reset %s: %v", requestID, err)
		}
	}
	if live := countLiveResetSecrets(t, pool); live != 1 {
		t.Fatalf("live reset secrets after reissue = %d, want 1", live)
	}

	firstDigest, err := DigestActionSecret(deterministicBearer(0x74))
	if err != nil {
		t.Fatalf("digesting first bearer: %v", err)
	}
	consumed, err := attemptConsumption(pool, firstDigest, now)
	if err != nil {
		t.Fatalf("superseded consumption: %v", err)
	}
	if consumed {
		t.Fatal("a superseded reset secret was still accepted")
	}
}

// plantSessionFamilies inserts live families directly so recovery has
// something real to invalidate.
func plantSessionFamilies(t *testing.T, pool *pgxpool.Pool, accountID string, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO sessions
			   (account_id, admitted_epoch, idle_expires_at, absolute_expires_at)
			 SELECT $1::uuid, session_epoch, now() + interval '1 day', now() + interval '7 days'
			   FROM accounts WHERE id = $1::uuid`,
			accountID,
		); err != nil {
			t.Fatalf("planting session family: %v", err)
		}
	}
}

func accountFacts(t *testing.T, pool *pgxpool.Pool) (id string, revision int, epoch int, hash string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT a.id::text, a.revision, a.session_epoch, c.password_hash
		   FROM accounts a JOIN password_credentials c ON c.account_id = a.id`,
	).Scan(&id, &revision, &epoch, &hash); err != nil {
		t.Fatalf("reading Account facts: %v", err)
	}
	return id, revision, epoch, hash
}

func issuedResetBearer(t *testing.T, pool *pgxpool.Pool, service *RecoveryService, fill byte) string {
	t.Helper()
	if err := service.RequestPasswordReset(context.Background(), PasswordResetRequest{
		Email: studentRegistration().Email, RequestID: "request-reset-1",
	}); err != nil {
		t.Fatalf("requesting reset: %v", err)
	}
	if live := countLiveResetSecrets(t, pool); live != 1 {
		t.Fatalf("live reset secrets after request = %d, want 1", live)
	}
	return deterministicBearer(fill)
}

// TestCompletePasswordResetIsAtomicAndInvalidatesEverySession is the Must 3
// stop condition: one transaction replaces the credential, revokes every
// family, advances revision and epoch, consumes the secret, and records
// evidence plus notification intent.
func TestCompletePasswordResetIsAtomicAndInvalidatesEverySession(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x81)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x91)
	accountID, revisionBefore, epochBefore, hashBefore := accountFacts(t, pool)
	plantSessionFamilies(t, pool, accountID, 3)
	bearer := issuedResetBearer(t, pool, service, 0x91)

	if err := service.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token:     bearer,
		Password:  config.NewSecret("a different sufficiently long passphrase"),
		RequestID: "request-complete-1",
	}); err != nil {
		t.Fatalf("completing reset: %v", err)
	}

	_, revisionAfter, epochAfter, hashAfter := accountFacts(t, pool)
	if hashAfter == hashBefore {
		t.Fatal("password hash was not replaced")
	}
	if revisionAfter != revisionBefore+1 {
		t.Fatalf("revision = %d, want %d", revisionAfter, revisionBefore+1)
	}
	if epochAfter != epochBefore+1 {
		t.Fatalf("session epoch = %d, want %d", epochAfter, epochBefore+1)
	}

	var live int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE account_id = $1::uuid AND state = 'ACTIVE'`,
		accountID,
	).Scan(&live); err != nil {
		t.Fatalf("counting live families: %v", err)
	}
	if live != 0 {
		t.Fatalf("live session families after recovery = %d, want 0", live)
	}
	var revokedForReset int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions
		  WHERE account_id = $1::uuid AND state = 'REVOKED'
		    AND revocation_reason = 'PASSWORD_RESET'`,
		accountID,
	).Scan(&revokedForReset); err != nil {
		t.Fatalf("counting revoked families: %v", err)
	}
	if revokedForReset != 3 {
		t.Fatalf("families revoked as PASSWORD_RESET = %d, want 3", revokedForReset)
	}

	// Recovery must not hand back a session: no family may be created by it.
	var created int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE account_id = $1::uuid`, accountID,
	).Scan(&created); err != nil {
		t.Fatalf("counting all families: %v", err)
	}
	if created != 3 {
		t.Fatalf("total families = %d, want the original 3 and no new session", created)
	}

	if live := countLiveResetSecrets(t, pool); live != 0 {
		t.Fatalf("live reset secrets after completion = %d, want 0", live)
	}
	var events int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_security_events
		  WHERE account_id = $1::uuid AND event_type = 'PASSWORD_RESET_COMPLETED'`,
		accountID,
	).Scan(&events); err != nil {
		t.Fatalf("counting completion evidence: %v", err)
	}
	if events != 1 {
		t.Fatalf("PASSWORD_RESET_COMPLETED events = %d, want 1", events)
	}
	var intents int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE event_type = 'identity.password_reset_completed'`,
	).Scan(&intents); err != nil {
		t.Fatalf("counting notification intent: %v", err)
	}
	if intents != 1 {
		t.Fatalf("completion outbox intents = %d, want 1", intents)
	}
}

// TestCompletedResetSecretCannotBeReplayed proves the consumed secret is spent
// for good, and that the second attempt changes nothing.
func TestCompletedResetSecretCannotBeReplayed(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x82)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x92)
	bearer := issuedResetBearer(t, pool, service, 0x92)

	completion := PasswordResetCompletion{
		Token:     bearer,
		Password:  config.NewSecret("a different sufficiently long passphrase"),
		RequestID: "request-complete-1",
	}
	if err := service.CompletePasswordReset(context.Background(), completion); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	_, revisionAfterFirst, epochAfterFirst, hashAfterFirst := accountFacts(t, pool)

	completion.RequestID = "request-complete-2"
	completion.Password = config.NewSecret("yet another sufficiently long phrase")
	if err := service.CompletePasswordReset(context.Background(), completion); !errors.Is(
		err, ErrTokenInvalid,
	) {
		t.Fatalf("replayed completion error = %v, want ErrTokenInvalid", err)
	}
	_, revisionAfterSecond, epochAfterSecond, hashAfterSecond := accountFacts(t, pool)
	if revisionAfterSecond != revisionAfterFirst ||
		epochAfterSecond != epochAfterFirst ||
		hashAfterSecond != hashAfterFirst {
		t.Fatal("a replayed reset mutated Account state")
	}
}

// TestSuspensionBetweenRequestAndCompletionBlocksRecovery proves eligibility is
// re-checked against the locked row at completion time, so a secret issued
// while an Account was active cannot revive it after suspension.
func TestSuspensionBetweenRequestAndCompletionBlocksRecovery(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x83)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x93)
	bearer := issuedResetBearer(t, pool, service, 0x93)

	accountID, _, _, hashBefore := accountFacts(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, accountID,
	); err != nil {
		t.Fatalf("suspending Account: %v", err)
	}

	if err := service.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token:     bearer,
		Password:  config.NewSecret("a different sufficiently long passphrase"),
		RequestID: "request-complete-1",
	}); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("suspended completion error = %v, want ErrTokenInvalid", err)
	}
	_, _, _, hashAfter := accountFacts(t, pool)
	if hashAfter != hashBefore {
		t.Fatal("a suspended Account's password was replaced by recovery")
	}
	if live := countLiveResetSecrets(t, pool); live != 1 {
		t.Fatalf("live reset secrets = %d; a refused completion must not consume", live)
	}
}

// TestWeakRecoveryPasswordIsRefusedWithoutConsumingTheSecret proves password
// policy runs before anything is spent, so a rejected password leaves the user
// able to try again with the same link.
func TestWeakRecoveryPasswordIsRefusedWithoutConsumingTheSecret(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x84)
	now := time.Now().UTC()
	service := recoveryService(t, pool, now, 0x94)
	bearer := issuedResetBearer(t, pool, service, 0x94)

	if err := service.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token:     bearer,
		Password:  config.NewSecret("short"),
		RequestID: "request-complete-1",
	}); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("weak password error = %v, want ErrPasswordPolicy", err)
	}
	if live := countLiveResetSecrets(t, pool); live != 1 {
		t.Fatalf("live reset secrets = %d; a refused password must not consume", live)
	}
}

// countingCompromisedSource records how many times screening was reached.
//
// screenCompromised runs immediately before HashPassword inside
// prepareCredential, and nothing else in the completion path calls it. A zero
// count therefore proves the Argon2id hash was never reached either — this is
// a structural consequence of that ordering, not an inference from timing.
type countingCompromisedSource struct {
	inner  CompromisedRangeSource
	mutex  sync.Mutex
	visits int
}

func (c *countingCompromisedSource) Scheme() CompromisedLookupScheme {
	return c.inner.Scheme()
}

func (c *countingCompromisedSource) PrefixLength() int { return c.inner.PrefixLength() }

func (c *countingCompromisedSource) Lookup(
	ctx context.Context,
	lookup CompromisedRangeLookup,
) (CompromisedRangeResult, error) {
	c.mutex.Lock()
	c.visits++
	c.mutex.Unlock()
	return c.inner.Lookup(ctx, lookup)
}

func (c *countingCompromisedSource) count() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.visits
}

// TestRefusedResetTokensNeverReachPasswordHashing guards the CPU-exhaustion
// boundary on the unauthenticated completion endpoint.
//
// Completion is the only anonymous route that can reach Argon2id. Without the
// preflight, posting arbitrary tokens would force a full hash plus a
// compromised-password screen per request at no cost to the attacker.
//
// The assertion is an invocation count on an injected screener, not elapsed
// time: a timing comparison is too environment-sensitive to be the proof.
func TestRefusedResetTokensNeverReachPasswordHashing(t *testing.T) {
	newPassword := config.NewSecret("a different sufficiently long passphrase")

	tests := []struct {
		scenario string
		// token returns the bearer to present, after any setup that makes it
		// unusable has been applied.
		token func(t *testing.T, pool *pgxpool.Pool, service *RecoveryService) string
	}{
		{
			scenario: "unknown",
			token: func(*testing.T, *pgxpool.Pool, *RecoveryService) string {
				return deterministicBearer(0xEE)
			},
		},
		{
			scenario: "wrong purpose",
			token: func(t *testing.T, pool *pgxpool.Pool, _ *RecoveryService) string {
				// A live EMAIL_VERIFICATION secret exists for this Account from
				// registration; present it to the recovery endpoint.
				return deterministicBearer(0x86)
			},
		},
		{
			scenario: "consumed",
			token: func(t *testing.T, pool *pgxpool.Pool, service *RecoveryService) string {
				bearer := issuedResetBearer(t, pool, service, 0x96)
				digest, err := DigestActionSecret(bearer)
				if err != nil {
					t.Fatalf("digesting bearer: %v", err)
				}
				consumed, err := attemptConsumption(pool, digest, time.Now().UTC())
				if err != nil || !consumed {
					t.Fatalf("pre-consuming secret = %v, %v", consumed, err)
				}
				return bearer
			},
		},
		{
			scenario: "superseded",
			token: func(t *testing.T, pool *pgxpool.Pool, service *RecoveryService) string {
				first := issuedResetBearer(t, pool, service, 0x96)
				// A second request supersedes the first.
				if err := service.RequestPasswordReset(
					context.Background(), PasswordResetRequest{
						Email: studentRegistration().Email, RequestID: "request-reset-2",
					},
				); err != nil {
					t.Fatalf("reissuing reset: %v", err)
				}
				return first
			},
		},
		{
			scenario: "expired",
			token: func(t *testing.T, pool *pgxpool.Pool, service *RecoveryService) string {
				// Expiry is reached by moving the completing service's clock
				// past the one-hour TTL, not by rewriting the row: the schema
				// refuses that, because action-secret issuance is immutable.
				return issuedResetBearer(t, pool, service, 0x96)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			pool := admissionPool(t)
			activeStudent(t, pool, 0x86)
			// The issuing service uses the real clock so "expired" can be made
			// to bite; the counting service is what the assertion watches.
			issuer := recoveryService(t, pool, time.Now().UTC(), 0x96)
			bearer := test.token(t, pool, issuer)

			screener := &countingCompromisedSource{inner: clearCompromisedSource()}
			writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x51}, 32))
			if err != nil {
				t.Fatalf("constructing outbox writer: %v", err)
			}
			guarded, err := NewRecoveryService(RecoveryServiceOptions{
				Pool: pool, Outbox: writer, Compromised: screener,
				ResetTTL: time.Hour,
				// Far enough ahead that an expired secret is unambiguously past
				// its window at completion time.
				Now:    func() time.Time { return time.Now().UTC().Add(2 * time.Hour) },
				Random: bytes.NewReader(bytes.Repeat([]byte{0xC1}, 32*8)),
			})
			if err != nil {
				t.Fatalf("constructing guarded recovery service: %v", err)
			}

			if err := guarded.CompletePasswordReset(
				context.Background(), PasswordResetCompletion{
					Token: bearer, Password: newPassword, RequestID: "request-refused-1",
				},
			); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("completion error = %v, want ErrTokenInvalid", err)
			}
			if visits := screener.count(); visits != 0 {
				t.Fatalf(
					"compromised-password screener called %d times for a %s secret; "+
						"refusal must happen before password hashing",
					visits, test.scenario,
				)
			}
		})
	}
}

// TestAcceptedResetReachesPasswordHashing is the counterpart: it proves the
// zero counts above come from the preflight refusing, not from the screener
// being unreachable in this harness.
func TestAcceptedResetReachesPasswordHashing(t *testing.T) {
	pool := admissionPool(t)
	activeStudent(t, pool, 0x87)
	now := time.Now().UTC()
	issuer := recoveryService(t, pool, now, 0x97)
	bearer := issuedResetBearer(t, pool, issuer, 0x97)

	screener := &countingCompromisedSource{inner: clearCompromisedSource()}
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	guarded, err := NewRecoveryService(RecoveryServiceOptions{
		Pool: pool, Outbox: writer, Compromised: screener,
		ResetTTL: time.Hour, Now: func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0xC2}, 32*8)),
	})
	if err != nil {
		t.Fatalf("constructing guarded recovery service: %v", err)
	}

	if err := guarded.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token:     bearer,
		Password:  config.NewSecret("a different sufficiently long passphrase"),
		RequestID: "request-accepted-1",
	}); err != nil {
		t.Fatalf("completing reset: %v", err)
	}
	if visits := screener.count(); visits != 1 {
		t.Fatalf("screener called %d times for an accepted reset, want 1", visits)
	}
}
