//go:build integration

package identity

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
		Pool:     pool,
		Outbox:   writer,
		ResetTTL: time.Hour,
		Now:      func() time.Time { return now },
		Random:   bytes.NewReader(randomness),
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
