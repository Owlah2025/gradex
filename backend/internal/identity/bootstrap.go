package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// bootstrapLockKey serializes concurrent bootstrap attempts.
//
// The singleton primary key on bootstrap_operations already makes a second
// completed bootstrap impossible, but on its own it makes concurrency resolve
// as a unique-violation crash after both transactions have done their work —
// including creating a second Admin Account that then rolls back. An advisory
// lock taken at the top of the transaction makes the loser wait and then
// observe the winner's completed row, which is the "retry returns the recorded
// result" behavior §4.5 requires rather than an error the operator has to
// interpret.
//
// The value is arbitrary but must never collide with another advisory lock in
// this application; it is registered here as the only Identity lock.
const bootstrapLockKey int64 = 0x6772616465785f31 // "gradex_1"

// Errors a caller may reasonably act on. Anything else is infrastructure.
var (
	// ErrBootstrapAlreadyCompleted means a bootstrap operation with a
	// *different* operation ID has already run. This is the fail-closed path:
	// the platform gets exactly one bootstrap Admin, ever.
	ErrBootstrapAlreadyCompleted = errors.New("a bootstrap operation has already completed")

	// ErrAdminAlreadyExists means an Admin Account exists without a recorded
	// bootstrap operation — a database restored from a system that predates the
	// marker, or manual intervention. Minting another Admin would be wrong, and
	// so would silently claiming this one, so it stops.
	ErrAdminAlreadyExists = errors.New("an Admin Account already exists")

	ErrInvalidEmail       = errors.New("email is not usable as a normalized address")
	ErrInvalidOperationID = errors.New("operation ID is missing or malformed")
	ErrInvalidDisplayName = errors.New("display name is missing")

	// ErrBootstrapRequestMismatch means the operation ID exists but at least
	// one semantic input differs from the completed operation.
	ErrBootstrapRequestMismatch = errors.New("bootstrap retry does not match the completed request")

	// ErrBootstrapRetryUnverifiable means a pre-fingerprint marker exists. It
	// fails closed instead of guessing that the submitted retry is identical.
	ErrBootstrapRetryUnverifiable = errors.New("bootstrap retry cannot be verified against the legacy marker")
)

const bootstrapFingerprintVersion int16 = 1

// BootstrapRequest is the complete input to the operation. Every field is
// required.
type BootstrapRequest struct {
	// OperationID is the caller's stable identifier for this attempt. Re-running
	// the command with the same ID is a retry; a different ID after a completed
	// bootstrap fails closed.
	OperationID string

	Email       string
	DisplayName string

	// Password arrives as a Secret because it came from the secret resolver and
	// must not be assignable from a flag by accident. It is hashed inside this
	// operation and never stored, logged, or returned.
	Password config.Secret

	// DeploymentPrincipal identifies who ran the deployment operation. It is
	// evidence, not authentication — the authorization is possession of the
	// database credential and the secret — and it is recorded in Audit so the
	// operation is attributable to something more specific than "a deployment".
	DeploymentPrincipal string

	// Compromised is required. An unavailable or unconfigured source fails
	// credential creation closed.
	Compromised CompromisedRangeSource
}

// BootstrapResult describes what the operation did, or what the earlier
// completed operation with the same ID did.
type BootstrapResult struct {
	AccountID   string
	Email       string
	OperationID string
	CompletedAt time.Time

	// AlreadyCompleted is true when this call was a retry that read back an
	// existing result rather than creating anything.
	AlreadyCompleted bool
}

const maxDisplayNameRunes = 200

// hashNewPassword validates and hashes a password with no existing credential
// to check against. The plaintext read itself happens in prepareCredential,
// which is the reviewed boundary.
func hashNewPassword(
	ctx context.Context,
	password config.Secret,
	source CompromisedRangeSource,
) (config.Secret, error) {
	return prepareCredential(ctx, credentialPreparation{
		next: password,
		mode: credentialPrepareNew,
	}, source)
}

// NormalizeEmail produces the form uniqueness is enforced on.
//
// Case folding and trimming only. The local part is deliberately left otherwise
// intact: stripping dots or +tags is provider-specific behavior, and applying
// Gmail's rules to every domain would silently merge addresses that are
// genuinely distinct elsewhere.
func NormalizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidEmail)
	}
	if utf8.RuneCountInString(trimmed) > 320 {
		return "", fmt.Errorf("%w: longer than 320 characters", ErrInvalidEmail)
	}

	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return "", fmt.Errorf("%w: must contain a local part and a domain", ErrInvalidEmail)
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return "", fmt.Errorf("%w: contains whitespace", ErrInvalidEmail)
	}
	if !strings.Contains(trimmed[at+1:], ".") {
		return "", fmt.Errorf("%w: domain has no dot", ErrInvalidEmail)
	}
	return strings.ToLower(trimmed), nil
}

// Bootstrap creates the one bootstrap Administrator inside a single
// transaction, per the API/security design §4.5.
//
// The whole operation is one transaction on purpose: the Account, its
// CHANGE_REQUIRED credential, the Audit evidence, and the completion marker are
// one fact. A partial result — an Admin with no marker, or a marker with no
// Admin — is worse than a failure, because the first is a second bootstrap
// waiting to happen and the second is a platform that can never bootstrap.
func Bootstrap(ctx context.Context, conn *pgx.Conn, req BootstrapRequest) (BootstrapResult, error) {
	operationID := strings.TrimSpace(req.OperationID)
	if operationID == "" {
		return BootstrapResult{}, fmt.Errorf("%w: empty", ErrInvalidOperationID)
	}
	if utf8.RuneCountInString(operationID) > 200 {
		return BootstrapResult{}, fmt.Errorf("%w: longer than 200 characters", ErrInvalidOperationID)
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return BootstrapResult{}, fmt.Errorf("%w: empty", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(displayName) > maxDisplayNameRunes {
		return BootstrapResult{}, fmt.Errorf("%w: longer than %d characters",
			ErrInvalidDisplayName, maxDisplayNameRunes)
	}

	correspondenceEmail := strings.TrimSpace(req.Email)
	normalizedEmail, err := NormalizeEmail(correspondenceEmail)
	if err != nil {
		return BootstrapResult{}, err
	}

	principal := strings.TrimSpace(req.DeploymentPrincipal)
	if principal == "" {
		return BootstrapResult{}, errors.New("deployment principal is required")
	}

	requestFingerprint := bootstrapRequestFingerprint(
		correspondenceEmail,
		normalizedEmail,
		displayName,
		principal,
	)

	// Validation and hashing happen behind one call so the plaintext is read
	// exactly once. Hashing is deliberately outside the transaction: Argon2id
	// at these parameters takes hundreds of milliseconds, and holding the
	// advisory lock across it would widen the window in which a concurrent
	// attempt waits for no reason.
	hash, err := hashNewPassword(ctx, req.Password, req.Compromised)
	if err != nil {
		return BootstrapResult{}, err
	}

	// READ COMMITTED, deliberately, in combination with the advisory lock below.
	//
	// SERIALIZABLE is the reflexive choice here and it is wrong for this
	// operation. A serializable transaction takes its snapshot at its first
	// statement — which is the lock acquisition — so a loser that waits on the
	// lock still reads the pre-lock snapshot afterwards, does not see the
	// winner's committed marker, proceeds to insert, and dies with a
	// serialization failure (40001). That surfaces to the operator as a crash
	// during a release rather than as "already bootstrapped", and it is exactly
	// what an integration test caught.
	//
	// Under READ COMMITTED each statement takes a fresh snapshot, so the loser
	// wakes up and observes the completed bootstrap. The advisory lock, not the
	// isolation level, is what provides mutual exclusion; the isolation level's
	// only job is to let the loser see the winner's work.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("beginning bootstrap transaction: %w", err)
	}
	// Rollback is unconditional and idempotent: after a successful Commit it
	// returns ErrTxClosed, which is why the error is ignored here rather than
	// reported.
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLockKey); err != nil {
		return BootstrapResult{}, fmt.Errorf("acquiring bootstrap lock: %w", err)
	}

	// Prove no bootstrap operation has completed. Holding the lock means this
	// read cannot be invalidated by a concurrent attempt before this
	// transaction commits.
	var (
		existingOperationID        string
		existingAccountID          string
		existingCompletedAt        time.Time
		existingFingerprintVersion *int16
		existingFingerprint        []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT operation_id, account_id::text, completed_at,
		        fingerprint_version, request_fingerprint
		   FROM bootstrap_operations WHERE singleton`,
	).Scan(
		&existingOperationID,
		&existingAccountID,
		&existingCompletedAt,
		&existingFingerprintVersion,
		&existingFingerprint,
	)

	switch {
	case err == nil:
		if existingOperationID == operationID {
			if existingFingerprintVersion == nil || existingFingerprint == nil {
				return BootstrapResult{}, ErrBootstrapRetryUnverifiable
			}
			if *existingFingerprintVersion != bootstrapFingerprintVersion ||
				subtle.ConstantTimeCompare(existingFingerprint, requestFingerprint) != 1 {
				return BootstrapResult{}, ErrBootstrapRequestMismatch
			}

			// A retry of the same operation. Read back the recorded result.
			var email, storedHash string
			if err := tx.QueryRow(ctx,
				`SELECT a.email, c.password_hash
				   FROM accounts a
				   JOIN password_credentials c ON c.account_id = a.id
				  WHERE a.id = $1`,
				existingAccountID,
			).Scan(&email, &storedHash); err != nil {
				return BootstrapResult{}, fmt.Errorf("reading bootstrapped Account: %w", err)
			}
			if err := verifyStoredCredential(ctx, req.Password, storedHash); err != nil {
				if errors.Is(err, errCredentialMismatch) {
					return BootstrapResult{}, ErrBootstrapRequestMismatch
				}
				return BootstrapResult{}, fmt.Errorf("verifying bootstrap retry credential: %w", err)
			}
			return BootstrapResult{
				AccountID:        existingAccountID,
				Email:            email,
				OperationID:      existingOperationID,
				CompletedAt:      existingCompletedAt,
				AlreadyCompleted: true,
			}, nil
		}
		// A different operation ID. Fail closed — this is the case that must
		// never mint a second Admin.
		return BootstrapResult{}, fmt.Errorf("%w (operation %q at %s)",
			ErrBootstrapAlreadyCompleted, existingOperationID, existingCompletedAt.UTC().Format(time.RFC3339))
	case errors.Is(err, pgx.ErrNoRows):
		// No bootstrap has run. Continue.
	default:
		return BootstrapResult{}, fmt.Errorf("reading bootstrap marker: %w", err)
	}

	// Prove no Admin Account exists. The marker being absent is not sufficient:
	// a database could carry an Admin from a restore or manual insert, and
	// creating a second one would break the single-bootstrap-Admin invariant
	// just as surely.
	var adminCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE role = 'ADMIN'`,
	).Scan(&adminCount); err != nil {
		return BootstrapResult{}, fmt.Errorf("counting existing Admin Accounts: %w", err)
	}
	if adminCount > 0 {
		return BootstrapResult{}, fmt.Errorf("%w: found %d", ErrAdminAlreadyExists, adminCount)
	}

	// The bootstrap Admin is created verified and ACTIVE — it has no mailbox
	// round trip to complete — and simultaneously CHANGE_REQUIRED on its
	// credential. Those are the two independent facts link 1's schema was
	// shaped to express.
	var accountID string
	var createdAt time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name, email_verified_at)
		 VALUES ($1, $2, 'ADMIN', 'ACTIVE', $3, now())
		 RETURNING id::text, created_at`,
		normalizedEmail, correspondenceEmail, displayName,
	).Scan(&accountID, &createdAt); err != nil {
		return BootstrapResult{}, fmt.Errorf("creating bootstrap Admin Account: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO password_credentials (account_id, password_hash, state)
		 VALUES ($1, $2, 'CHANGE_REQUIRED')`,
		accountID, hash.Expose(),
	); err != nil {
		return BootstrapResult{}, fmt.Errorf("creating bootstrap credential: %w", err)
	}

	// Audit evidence, in the same transaction as the change it evidences
	// (domain design §17). The metadata carries no credential material: the
	// operation ID, the principal, and the normalized email are all values that
	// already appear in operational records.
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_events
		   (actor_account_id, actor_role, actor_descriptor, action, module,
		    target_type, target_id, reason, metadata, operation_id)
		 VALUES (NULL, 'SYSTEM', $1, 'BOOTSTRAP_ADMIN_CREATED', 'IDENTITY_AND_ACCESS',
		         'ACCOUNT', $2, $3, jsonb_build_object(
		             'normalized_email', $4::text,
		             'credential_state', 'CHANGE_REQUIRED',
		             'account_status', 'ACTIVE'
		         ), $5)`,
		principal,
		accountID,
		"Secure deployment bootstrap of the initial Administrator",
		normalizedEmail,
		operationID,
	); err != nil {
		return BootstrapResult{}, fmt.Errorf("recording bootstrap audit event: %w", err)
	}

	var completedAt time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO bootstrap_operations
		   (operation_id, account_id, fingerprint_version, request_fingerprint)
		 VALUES ($1, $2, $3, $4)
		 RETURNING completed_at`,
		operationID, accountID, bootstrapFingerprintVersion, requestFingerprint,
	).Scan(&completedAt); err != nil {
		return BootstrapResult{}, fmt.Errorf("recording bootstrap completion: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("committing bootstrap transaction: %w", err)
	}

	return BootstrapResult{
		AccountID:   accountID,
		Email:       correspondenceEmail,
		OperationID: operationID,
		CompletedAt: completedAt,
	}, nil
}

func bootstrapRequestFingerprint(
	correspondenceEmail string,
	normalizedEmail string,
	displayName string,
	deploymentPrincipal string,
) []byte {
	h := sha256.New()
	writeFingerprintField(h, "command", "bootstrap-admin")
	writeFingerprintField(h, "version", fmt.Sprint(bootstrapFingerprintVersion))
	writeFingerprintField(h, "correspondence_email", correspondenceEmail)
	writeFingerprintField(h, "normalized_email", normalizedEmail)
	writeFingerprintField(h, "display_name", displayName)
	writeFingerprintField(h, "deployment_principal", deploymentPrincipal)
	return h.Sum(nil)
}

func writeFingerprintField(h hash.Hash, name string, value string) {
	var size [4]byte
	for _, field := range []string{name, value} {
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(field))
	}
}
