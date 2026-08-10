package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("transactional email database is required")
	}
	return &Repository{pool: pool}, nil
}

type Claim struct {
	Event         outbox.Event
	Template      string
	Locale        string
	AttemptNumber int
	LeaseToken    string
	Protected     outbox.StoredProtectedPayload
}

type claimBatch struct {
	provider string
	now      time.Time
	lease    time.Duration
	limit    int
}

type ClaimOptions struct {
	Provider      string
	Now           time.Time
	LeaseDuration time.Duration
	Limit         int
}

type claimCandidate struct {
	eventID string
	attempt int
	status  string
	lease   *string
}

func (r *Repository) Claim(ctx context.Context, options ClaimOptions) ([]Claim, error) {
	batch := claimBatch{provider: options.Provider, now: options.Now, lease: options.LeaseDuration, limit: options.Limit}
	if err := batch.validate(); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transactional email claim: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := discoverDeliveries(ctx, tx, batch); err != nil {
		return nil, err
	}
	if err := exhaustStaleDeliveries(ctx, tx, batch.now); err != nil {
		return nil, err
	}
	candidates, err := selectClaimCandidates(ctx, tx, batch)
	if err != nil {
		return nil, err
	}
	claims := make([]Claim, 0, len(candidates))
	for _, candidate := range candidates {
		claim, err := claimDelivery(ctx, tx, candidate, batch)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing transactional email claims: %w", err)
	}
	return claims, nil
}

func (b claimBatch) validate() error {
	if b.provider != "fake" && b.provider != "resend" {
		return errors.New("transactional email provider is unsupported")
	}
	if b.lease <= 0 {
		return errors.New("transactional email lease must be positive")
	}
	if b.limit < 1 || b.limit > 100 {
		return errors.New("transactional email claim limit must be between 1 and 100")
	}
	return nil
}

// requireActivation reads the durable activation boundary. Workers never write
// it — migration 0017 stamps it once — so a poller cannot advance it by
// restarting, and every concurrent poller reads the same instant.
//
// An absent boundary is an error rather than a default, because both defaults
// are wrong: "beginning of time" mails the entire history, and "now" silently
// drops live intents on every tick.
func requireActivation(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var activatedAt time.Time
	err := tx.QueryRow(ctx,
		`SELECT activated_at FROM transactional_email_activation WHERE id`).Scan(&activatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errors.New("transactional email activation boundary is missing")
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("reading transactional email activation boundary: %w", err)
	}
	return activatedAt, nil
}

func discoverDeliveries(ctx context.Context, tx pgx.Tx, batch claimBatch) error {
	contracts, err := json.Marshal(eventTemplates)
	if err != nil {
		return errors.New("transactional email contracts cannot be encoded")
	}
	// occurred_at, not available_at: the boundary asks when the domain intent
	// was created, not when it became due. A deliberately delayed intent
	// created after activation stays eligible.
	//
	activatedAt, err := requireActivation(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO transactional_email_deliveries
		       (event_id, template_contract, locale, provider, next_attempt_at, queued_at, created_at, updated_at)
		SELECT e.id, e.safe_payload->>'template_contract', e.safe_payload->>'locale', $1, e.available_at, $2, $2, $2
		  FROM outbox_events e
		  JOIN jsonb_each_text($3::jsonb) c(event_type, template_contract)
		    ON c.event_type=e.event_type AND c.template_contract=e.safe_payload->>'template_contract'
		 WHERE e.safe_payload->>'locale' IN ('ar', 'en')
		   AND e.occurred_at >= $4
		ON CONFLICT (event_id) DO NOTHING`, batch.provider, batch.now, contracts, activatedAt)
	if err != nil {
		return fmt.Errorf("discovering transactional email intents: %w", err)
	}
	return nil
}

func exhaustStaleDeliveries(ctx context.Context, tx pgx.Tx, now time.Time) error {
	_, err := tx.Exec(ctx, `
		WITH stale AS (
			SELECT event_id, attempt_count, lease_token
			  FROM transactional_email_deliveries
			 WHERE status='SENDING' AND lease_expires_at <= $1 AND attempt_count >= 5
			 FOR UPDATE
		), closed_attempts AS (
			UPDATE transactional_email_attempts a
			   SET outcome='EXHAUSTED', failure_class='lease_expired', finished_at=$1
			  FROM stale s
			 WHERE a.event_id=s.event_id AND a.attempt_number=s.attempt_count
			   AND a.lease_token=s.lease_token AND a.outcome='STARTED'
			 RETURNING a.event_id
		)
		UPDATE transactional_email_deliveries d
		   SET status='EXHAUSTED', terminal_at=$1, lease_token=NULL, lease_expires_at=NULL,
		       last_failure_class='lease_expired', updated_at=$1
		  FROM stale s
		 WHERE d.event_id=s.event_id`, now)
	if err != nil {
		return fmt.Errorf("exhausting stale transactional email attempts: %w", err)
	}
	return nil
}

func selectClaimCandidates(ctx context.Context, tx pgx.Tx, batch claimBatch) ([]claimCandidate, error) {
	rows, err := tx.Query(ctx, `
		SELECT event_id::text, attempt_count, status, lease_token::text
		  FROM transactional_email_deliveries
		 WHERE provider=$1 AND attempt_count < 5
		   AND ((status='QUEUED' AND next_attempt_at <= $2)
		        OR (status='SENDING' AND lease_expires_at <= $2))
		 ORDER BY next_attempt_at, event_id
		 FOR UPDATE SKIP LOCKED LIMIT $3`, batch.provider, batch.now, batch.limit)
	if err != nil {
		return nil, fmt.Errorf("selecting transactional email claims: %w", err)
	}
	defer rows.Close()
	var candidates []claimCandidate
	for rows.Next() {
		var c claimCandidate
		if err := rows.Scan(&c.eventID, &c.attempt, &c.status, &c.lease); err != nil {
			return nil, fmt.Errorf("reading transactional email claim: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating transactional email claims: %w", err)
	}
	return candidates, nil
}

func closeStaleAttempt(ctx context.Context, tx pgx.Tx, candidate claimCandidate, now time.Time) error {
	if candidate.lease == nil {
		return errors.New("stale transactional email claim is missing its lease")
	}
	result, err := tx.Exec(ctx, `UPDATE transactional_email_attempts
				SET outcome='TRANSIENT_FAILURE',failure_class='lease_expired',retry_at=$1,finished_at=$1
				WHERE event_id=$2::uuid AND attempt_number=$3 AND lease_token=$4::uuid AND outcome='STARTED'`,
		now, candidate.eventID, candidate.attempt, *candidate.lease)
	if err != nil {
		return fmt.Errorf("closing stale transactional email attempt: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("stale transactional email attempt is not current")
	}
	return nil
}

func claimDelivery(ctx context.Context, tx pgx.Tx, candidate claimCandidate, batch claimBatch) (Claim, error) {
	if candidate.status == "SENDING" {
		if err := closeStaleAttempt(ctx, tx, candidate, batch.now); err != nil {
			return Claim{}, err
		}
	}
	leaseToken := uuid.NewString()
	attempt := candidate.attempt + 1
	if _, err := tx.Exec(ctx, `UPDATE transactional_email_deliveries
			SET status='SENDING', attempt_count=$1, lease_token=$2::uuid, lease_expires_at=$3,
			    last_failure_class=NULL, last_provider_code=NULL, updated_at=$4
			WHERE event_id=$5::uuid`, attempt, leaseToken, batch.now.Add(batch.lease), batch.now, candidate.eventID); err != nil {
		return Claim{}, fmt.Errorf("claiming transactional email: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transactional_email_attempts
			(event_id, attempt_number, lease_token, started_at) VALUES ($1::uuid,$2,$3::uuid,$4)`,
		candidate.eventID, attempt, leaseToken, batch.now); err != nil {
		return Claim{}, fmt.Errorf("recording transactional email attempt: %w", err)
	}
	return loadClaim(ctx, tx, claimIdentity{eventID: candidate.eventID, attempt: attempt, leaseToken: leaseToken})
}

type claimIdentity struct {
	eventID    string
	attempt    int
	leaseToken string
}

func loadClaim(ctx context.Context, tx pgx.Tx, identity claimIdentity) (Claim, error) {
	var claim Claim
	var safePayload []byte
	claim.AttemptNumber, claim.LeaseToken = identity.attempt, identity.leaseToken
	err := tx.QueryRow(ctx, `SELECT e.id::text,e.event_type,e.schema_version,e.source_module,e.aggregate_type,
			e.aggregate_id::text,e.aggregate_revision,e.correlation_id,e.available_at,e.safe_payload,
			d.template_contract,d.locale,p.key_version,p.nonce,p.ciphertext
			FROM outbox_events e JOIN outbox_protected_payloads p ON p.event_id=e.id
			JOIN transactional_email_deliveries d ON d.event_id=e.id WHERE e.id=$1::uuid`, identity.eventID).Scan(
		&claim.Event.ID, &claim.Event.Type, &claim.Event.SchemaVersion, &claim.Event.SourceModule, &claim.Event.AggregateType,
		&claim.Event.AggregateID, &claim.Event.AggregateRevision, &claim.Event.CorrelationID, &claim.Event.AvailableAt, &safePayload,
		&claim.Template, &claim.Locale, &claim.Protected.KeyVersion, &claim.Protected.Nonce, &claim.Protected.Ciphertext)
	if err != nil {
		return Claim{}, fmt.Errorf("loading transactional email claim payload: %w", err)
	}
	var safe any
	if err := json.Unmarshal(safePayload, &safe); err != nil {
		return Claim{}, errors.New("transactional email safe payload is malformed")
	}
	claim.Event.SafePayload = safe
	return claim, nil
}

func (r *Repository) Accept(ctx context.Context, claim Claim, providerMessageID string, now time.Time) error {
	if !safeProviderValue.MatchString(providerMessageID) {
		return errors.New("transactional email provider message ID is invalid")
	}
	return r.completeDeliveryAttempt(ctx, completion{claim: claim, status: "ACCEPTED", providerID: providerMessageID, now: now})
}

func (r *Repository) Fail(ctx context.Context, claim Claim, failure *SendFailure, now time.Time) error {
	if failure == nil {
		failure = &SendFailure{Kind: FailurePermanent, Class: "unknown", Code: "unknown"}
	}
	class, code := safeClass(failure.Class), safeClass(failure.Code)
	if failure.Transient() && claim.AttemptNumber < MaxAttempts {
		delay := retryDelay(claim.AttemptNumber)
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		retryAt := now.Add(delay)
		return r.completeDeliveryAttempt(ctx, completion{claim: claim, status: "QUEUED", class: class, code: code, retryAt: &retryAt, now: now})
	}
	status := "PERMANENT_FAILED"
	if failure.Transient() {
		status = "EXHAUSTED"
	}
	return r.completeDeliveryAttempt(ctx, completion{claim: claim, status: status, class: class, code: code, now: now})
}

type completion struct {
	claim      Claim
	status     string
	class      string
	code       string
	providerID string
	retryAt    *time.Time
	now        time.Time
}

func (r *Repository) completeDeliveryAttempt(ctx context.Context, completed completion) error {
	if strings.TrimSpace(completed.claim.Event.ID) == "" || strings.TrimSpace(completed.claim.LeaseToken) == "" {
		return errors.New("transactional email claim identity is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transactional email completion: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := completeAttempt(ctx, tx, completed); err != nil {
		return err
	}
	if err := completeDelivery(ctx, tx, completed); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transactional email completion: %w", err)
	}
	return nil
}

func completeAttempt(ctx context.Context, tx pgx.Tx, completed completion) error {
	outcome := completed.status
	switch completed.status {
	case "QUEUED":
		outcome = "TRANSIENT_FAILURE"
	case "PERMANENT_FAILED":
		outcome = "PERMANENT_FAILURE"
	}
	result, err := tx.Exec(ctx, `UPDATE transactional_email_attempts SET outcome=$1,failure_class=NULLIF($2,''),
		provider_code=NULLIF($3,''),provider_message_id=NULLIF($4,''),retry_at=$5,finished_at=$6
		WHERE event_id=$7::uuid AND attempt_number=$8 AND lease_token=$9::uuid AND outcome='STARTED'`,
		outcome, completed.class, completed.code, completed.providerID, completed.retryAt, completed.now,
		completed.claim.Event.ID, completed.claim.AttemptNumber, completed.claim.LeaseToken)
	if err != nil {
		return fmt.Errorf("completing transactional email attempt: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("transactional email attempt lease is no longer current")
	}
	return nil
}

func completeDelivery(ctx context.Context, tx pgx.Tx, completed completion) error {
	var acceptedAt, terminalAt *time.Time
	if completed.status == "ACCEPTED" {
		acceptedAt = &completed.now
	}
	if completed.status == "PERMANENT_FAILED" || completed.status == "EXHAUSTED" {
		terminalAt = &completed.now
	}
	nextAttempt := completed.now
	if completed.retryAt != nil {
		nextAttempt = *completed.retryAt
	}
	result, err := tx.Exec(ctx, `UPDATE transactional_email_deliveries SET status=$1,next_attempt_at=$2,
		lease_token=NULL,lease_expires_at=NULL,provider_message_id=NULLIF($3,''),last_failure_class=NULLIF($4,''),
		last_provider_code=NULLIF($5,''),accepted_at=$6,terminal_at=$7,updated_at=$8
		WHERE event_id=$9::uuid AND status='SENDING' AND lease_token=$10::uuid AND attempt_count=$11`,
		completed.status, nextAttempt, completed.providerID, completed.class, completed.code, acceptedAt, terminalAt,
		completed.now, completed.claim.Event.ID, completed.claim.LeaseToken, completed.claim.AttemptNumber)
	if err != nil {
		return fmt.Errorf("completing transactional email delivery: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("transactional email delivery lease is no longer current")
	}
	return nil
}
