//go:build integration

package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// D-103 H1 (second pass). The first remediation answered "is this operation
// spent?" with one statement:
//
//	SELECT EXISTS (SELECT 1 FROM processing_attempts WHERE ...)
//	FROM media_asset_versions WHERE id = $1 FOR UPDATE
//
// PostgreSQL evaluates that uncorrelated EXISTS as an InitPlan at executor
// start — before the scan that takes FOR UPDATE — and does not re-evaluate it
// when the lock wait ends. Under READ COMMITTED the row itself is re-read after
// blocking, but the InitPlan answer is not, so the transaction proceeds on a
// decision made before it held the lock.
//
// That is the release-blocking interleaving these tests force. They do not
// sleep-and-hope: the stale operation is started in a goroutine, and the test
// waits until PostgreSQL itself reports that backend blocked on a lock before
// letting recovery commit.

// waitForBlockedBackend blocks until PostgreSQL reports at least `want` backends
// in this database waiting on a lock. This is what makes the interleaving
// deterministic rather than timing-dependent.
func waitForBlockedBackend(t *testing.T, f *mediaFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		if err := f.pool.QueryRow(f.ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND state = 'active'
			  AND pid <> pg_backend_pid()
		`).Scan(&blocked); err != nil {
			t.Fatalf("reading blocked backends: %v", err)
		}
		if blocked >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("no backend blocked on a row lock within the deadline; the interleaving was not reached")
}

// lockedStaleWork takes the authoritative row lock inside tx and returns the
// staleWork record the real recovery path operates on, so the test drives
// production recovery code rather than a hand-written imitation of it.
func lockedStaleWork(t *testing.T, f *mediaFixture, tx pgx.Tx, assetVersionID string) staleWork {
	t.Helper()
	var work staleWork
	var leaseStillLive bool
	if err := tx.QueryRow(f.ctx, `
		SELECT id::text, kind, state, work_claim_token, active_processing_attempt_kind::text,
		       COALESCE(work_lease_expires_at > now(), false),
		       scan_attempt_count, processing_attempt_count,
		       successful_validation_attempt_id IS NOT NULL
		FROM media_asset_versions WHERE id=$1::uuid FOR UPDATE
	`, assetVersionID).Scan(&work.id, &work.kind, &work.state, &work.token, &work.processingKind, &leaseStillLive,
		&work.scanAttempts, &work.processingAttempts, &work.hasValidationEvidence); err != nil {
		t.Fatalf("locking stale work: %v", err)
	}
	if leaseStillLive {
		t.Fatal("the lease is still live; the test did not reach the stale precondition")
	}
	return work
}

// TestD103StaleFailureLosesTheLockRaceAndMutatesNothing is the exact sequence
// the independent review described:
//
//  1. stale operation A reaches the point immediately before the row lock;
//  2. recovery owns the lock;
//  3. recovery records A terminal and creates replacement B;
//  4. recovery commits;
//  5. A resumes and must observe that it is spent;
//  6. A performs no mutation, and B is unharmed.
//
// A arrives through recordProcessingFailure, which is how a real stale worker
// re-enters: it claimed, its processor ran past the lease, recovery superseded
// it, and only then did its processor return an error.
func TestD103StaleFailureLosesTheLockRaceAndMutatesNothing(t *testing.T) {
	f, worker, _, versionID := validatedVideoReadyForProcessing(t)

	operationA := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operationA); err != nil || !applied {
		t.Fatalf("operation A beginTranscode applied=%t err=%v", applied, err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count after A claimed = %d, want 1", got)
	}
	expireLease(t, f, versionID)

	// Recovery takes the authoritative lock and holds it.
	recoveryTx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatalf("beginning recovery transaction: %v", err)
	}
	defer func() { _ = recoveryTx.Rollback(f.ctx) }()
	work := lockedStaleWork(t, f, recoveryTx, versionID)

	// A starts and blocks on that lock.
	staleResult := make(chan error, 1)
	go func() {
		staleResult <- worker.recordProcessingFailure(
			context.Background(), versionID, operationA, errors.New("injected stale processor failure"))
	}()
	waitForBlockedBackend(t, f, 1)

	// Recovery supersedes A and queues B, then commits, all while A waits.
	if err := worker.recoverStaleProcessing(f.ctx, recoveryTx, work); err != nil {
		t.Fatalf("recoverStaleProcessing: %v", err)
	}
	if err := recoveryTx.Commit(f.ctx); err != nil {
		t.Fatalf("committing recovery: %v", err)
	}

	// A resumes holding the lock and must refuse.
	var staleErr error
	select {
	case staleErr = <-staleResult:
	case <-time.After(30 * time.Second):
		t.Fatal("stale operation A never completed")
	}

	// The mutations are asserted before the returned error, so a regression
	// reports the damage it did rather than only how it exited.
	// A mutated nothing.
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state after the stale failure resumed = %s, want VALIDATED — A mutated the replacement's state", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count = %d, want 1 — A consumed the replacement's attempt budget", got)
	}
	if got := claimToken(t, f, versionID); got != "" {
		t.Fatalf("claim token = %q, want none — A overwrote the replacement's lease token", got)
	}
	if got := processingAttemptRows(t, f, versionID); got != 1 {
		t.Fatalf("processing attempt rows = %d, want 1 — A wrote a second terminal row", got)
	}
	var recordedState string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT state::text FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id=$2
	`, versionID, operationA).Scan(&recordedState); err != nil {
		t.Fatalf("loading A's terminal record: %v", err)
	}
	if recordedState != "FAILED" {
		t.Fatalf("A's recorded state = %s, want the recovery-written FAILED", recordedState)
	}
	if staleErr != nil {
		t.Fatalf("stale operation A returned an error instead of a harmless no-op: %v", staleErr)
	}

	// B remains claimable and completes.
	operationB := transcodeOperationID(t, f, versionID, 0)
	if operationB == operationA {
		t.Fatal("recovery reused the superseded identity")
	}
	if err := worker.Transcode(f.ctx, versionID, operationB); err != nil {
		t.Fatalf("replacement operation B: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state after B = %s, want READY", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 2 {
		t.Fatalf("attempt count after B = %d, want 2", got)
	}
}

// TestD103StaleClaimLosesTheLockRaceAndMutatesNothing is the same interleaving
// on the claim path: a redelivery of A blocks on the lock, recovery supersedes A
// and queues B, and A must not acquire processing authority when it resumes.
func TestD103StaleClaimLosesTheLockRaceAndMutatesNothing(t *testing.T) {
	f, worker, _, versionID := validatedVideoReadyForProcessing(t)

	operationA := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operationA); err != nil || !applied {
		t.Fatalf("operation A beginTranscode applied=%t err=%v", applied, err)
	}
	expireLease(t, f, versionID)

	recoveryTx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatalf("beginning recovery transaction: %v", err)
	}
	defer func() { _ = recoveryTx.Rollback(f.ctx) }()
	work := lockedStaleWork(t, f, recoveryTx, versionID)

	claimResult := make(chan error, 1)
	go func() {
		claimResult <- worker.Transcode(context.Background(), versionID, operationA)
	}()
	waitForBlockedBackend(t, f, 1)

	if err := worker.recoverStaleProcessing(f.ctx, recoveryTx, work); err != nil {
		t.Fatalf("recoverStaleProcessing: %v", err)
	}
	if err := recoveryTx.Commit(f.ctx); err != nil {
		t.Fatalf("committing recovery: %v", err)
	}

	select {
	case err := <-claimResult:
		if err != nil {
			t.Fatalf("redelivered operation A returned an error instead of a no-op: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("redelivered operation A never completed")
	}

	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state = %s, want VALIDATED — A reacquired processing authority", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count = %d, want 1 — A consumed the replacement's budget", got)
	}
	if got := claimToken(t, f, versionID); got != "" {
		t.Fatalf("claim token = %q, want none", got)
	}

	operationB := transcodeOperationID(t, f, versionID, 0)
	if err := worker.Transcode(f.ctx, versionID, operationB); err != nil {
		t.Fatalf("replacement operation B: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state after B = %s, want READY", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 2 {
		t.Fatalf("attempt count after B = %d, want 2", got)
	}
}
