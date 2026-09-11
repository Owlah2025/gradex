package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

var (
	ErrAuthenticationFailed   = errors.New("authentication failed")
	ErrAuthenticationRequired = errors.New("authentication is required")
	ErrSessionReplaced        = errors.New("session was replaced")
	ErrSessionReuseDetected   = errors.New("session credential reuse was detected")
	ErrSessionCSRFFailed      = errors.New("session CSRF validation failed")
)

const dummyLoginPassword = "gradex timing equalization credential"

// SessionRepositoryOptions is the complete server-side session authority.
type SessionRepositoryOptions struct {
	Pool                     *pgxpool.Pool
	Settings                 config.SessionSettings
	CSRFKey                  []byte
	Now                      func() time.Time
	PasswordVerificationGate *PasswordVerificationGate
	// Devices decides what device authority a new Student family carries.
	//
	// Optional at construction because Instructor-only and fixture wiring have
	// no use for it, and absent it the repository fails *closed*: a Student
	// family is created PENDING_DEVICE_TRUST, which grants the self-service set
	// and nothing else. A deployment that forgets to wire device policy
	// therefore stops protected learning rather than silently exempting every
	// Student from the limit.
	Devices *DeviceService
}

// SessionRepository owns family and immutable-generation transactions.
type SessionRepository struct {
	pool                     *pgxpool.Pool
	settings                 config.SessionSettings
	csrfKey                  []byte
	now                      func() time.Time
	dummyHash                string
	passwordVerificationGate *PasswordVerificationGate
	devices                  *DeviceService
}

// LoginRequest contains the only user-supplied credential boundary.
type LoginRequest struct {
	Email     string
	Password  config.Secret
	RequestID string

	// DeviceCredentialDigest is the digest of the device credential the browser
	// presented, or empty when it presented none. The plaintext never reaches
	// this package: the HTTP boundary digests it and mints a replacement when
	// there was nothing to digest.
	DeviceCredentialDigest string
	// UserAgent names the device for the Student. It is never identity.
	UserAgent string
	// SourceAddress is recorded for security forensics only and is never
	// compared to decide whether a browser matches a device.
	SourceAddress string
}

// AuthenticatedSession contains no bearer or CSRF plaintext.
type AuthenticatedSession struct {
	AccountID string
	SessionID string
	// DeviceTrust and TrustedDeviceID answer "account -> active session ->
	// trusted device" on every authenticated request. They are re-derived from
	// the device record on each read rather than trusted from the session row,
	// so revoking a device takes effect on the very next request.
	DeviceTrust       SessionDeviceTrust
	TrustedDeviceID   string
	DisplayName       string
	Role              Role
	CredentialState   CredentialState
	Generation        int
	AuthenticatedAt   time.Time
	ReauthenticatedAt *time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

// SessionGrant is returned only after a credential-changing transaction
// commits.
type SessionGrant struct {
	Session    AuthenticatedSession
	Credential config.Secret
	CSRFToken  config.Secret
	// Device is present when device policy applied to this grant. It tells the
	// HTTP boundary whether to challenge the browser, show the device limit, or
	// carry on, and never contains a credential or a code.
	Device *DeviceAdmissionResult
}

// SessionView restores browser-memory CSRF state without rotating.
type SessionView struct {
	Session   AuthenticatedSession
	CSRFToken config.Secret
}

// SessionMutation identifies one cookie-authenticated state change.
type SessionMutation struct {
	CredentialDigest string
	CSRFDigest       string
	RequestID        string
}

func NewSessionRepository(options SessionRepositoryOptions) (*SessionRepository, error) {
	if options.Pool == nil || options.Now == nil {
		return nil, errors.New("session repository pool and clock are required")
	}
	if len(options.CSRFKey) < sessionCredentialBytes {
		return nil, errors.New("session repository CSRF key must contain at least 32 bytes")
	}
	dummyHash, err := hashPasswordEncoded(dummyLoginPassword)
	if err != nil {
		return nil, fmt.Errorf("creating dummy login credential: %w", err)
	}
	gate := options.PasswordVerificationGate
	if gate == nil {
		gate, err = NewPasswordVerificationGate(PasswordVerificationGateOptions{
			Concurrency: config.DefaultLoginPasswordVerificationConcurrency,
			Queue:       config.DefaultLoginPasswordVerificationQueue,
			QueueWait:   config.DefaultLoginPasswordVerificationQueueWait,
		})
		if err != nil {
			return nil, err
		}
	}
	return &SessionRepository{
		pool:                     options.Pool,
		settings:                 options.Settings,
		csrfKey:                  append([]byte(nil), options.CSRFKey...),
		now:                      options.Now,
		dummyHash:                dummyHash,
		passwordVerificationGate: gate,
		devices:                  options.Devices,
	}, nil
}

// AttachDevices wires device policy after construction.
//
// The two services are mutually dependent — the session authority decides a new
// family's device state, and device trust binds an existing family — and one of
// the two edges has to be closed after both exist. This is that edge, and it is
// deliberately the narrower one: it can only ever tighten what a new Student
// family is granted.
func (r *SessionRepository) AttachDevices(devices *DeviceService) { r.devices = devices }

type loginCandidate struct {
	accountID       string
	displayName     string
	role            Role
	status          AccountStatus
	credentialState CredentialState
	sessionEpoch    int
	revision        int
	verifiedAt      *time.Time
	passwordHash    string
	// email and locale are read for the device-trust challenge only. They are
	// never returned to the caller and never appear in a login response.
	email  string
	locale Locale
}

func (r *SessionRepository) Login(
	ctx context.Context,
	request LoginRequest,
) (SessionGrant, error) {
	candidateStarted := time.Now()
	candidate, found, err := r.loginCandidate(ctx, request.Email)
	observeLoginTiming(ctx, LoginStageCandidateLookup, candidateStarted)
	if err != nil {
		return SessionGrant{}, err
	}
	hash := r.dummyHash
	if found {
		hash = candidate.passwordHash
	}
	gateStarted := time.Now()
	verificationStarted := false
	if err := r.passwordVerificationGate.Verify(ctx, func() error {
		verificationStarted = true
		observeLoginTiming(ctx, LoginStageGateWait, gateStarted)
		verifyStarted := time.Now()
		err := verifyStoredCredential(ctx, request.Password, hash)
		observeLoginTiming(ctx, LoginStagePasswordVerify, verifyStarted)
		return err
	}); err != nil {
		if !verificationStarted {
			observeLoginTiming(ctx, LoginStageGateWait, gateStarted)
		}
		if errors.Is(err, ErrPasswordVerificationSaturated) ||
			errors.Is(err, ErrPasswordVerificationTimeout) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SessionGrant{}, err
		}
		return SessionGrant{}, errors.Join(
			ErrAuthenticationFailed,
			fmt.Errorf("verifying login credential: %w", err),
		)
	}
	if !found || !candidate.loginAllowed() {
		return SessionGrant{}, ErrAuthenticationFailed
	}
	return r.createSession(ctx, request, candidate)
}

func (r *SessionRepository) loginCandidate(
	ctx context.Context,
	rawEmail string,
) (loginCandidate, bool, error) {
	normalized, err := NormalizeEmail(rawEmail)
	if err != nil {
		return loginCandidate{}, false, nil
	}
	var candidate loginCandidate
	err = r.pool.QueryRow(ctx,
		`SELECT a.id::text, a.display_name, a.role::text, a.status::text,
		        c.state::text, a.session_epoch, a.revision, a.email_verified_at,
		        c.password_hash, a.email, a.locale
		   FROM accounts a
		   JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.normalized_email = $1`,
		normalized,
	).Scan(
		&candidate.accountID,
		&candidate.displayName,
		&candidate.role,
		&candidate.status,
		&candidate.credentialState,
		&candidate.sessionEpoch,
		&candidate.revision,
		&candidate.verifiedAt,
		&candidate.passwordHash,
		&candidate.email,
		&candidate.locale,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return loginCandidate{}, false, nil
	}
	if err != nil {
		return loginCandidate{}, false, fmt.Errorf("loading login candidate: %w", err)
	}
	return candidate, true, nil
}

func (c loginCandidate) loginAllowed() bool {
	return c.status == StatusActive &&
		c.verifiedAt != nil &&
		c.role.Valid() &&
		c.credentialState.Valid()
}

func (r *SessionRepository) createSession(
	ctx context.Context,
	request LoginRequest,
	candidate loginCandidate,
) (SessionGrant, error) {
	prepareStarted := time.Now()
	pending, err := r.prepareSession(candidate)
	observeLoginTiming(ctx, LoginStageSessionPrepare, prepareStarted)
	if err != nil {
		return SessionGrant{}, err
	}
	// The protected-payload nonce is the one fallible entropy read that must
	// happen before the transaction opens, exactly as registration does it. A
	// login that cannot reserve one is refused rather than committing a device
	// challenge whose code could never be mailed.
	reservation, err := r.reserveDeviceChallengePayload(ctx, candidate)
	if err != nil {
		return SessionGrant{}, err
	}

	writeStarted := time.Now()
	admission, err := r.persistSession(ctx, request, candidate, pending, reservation)
	if err != nil {
		observeLoginTiming(ctx, LoginStageSessionWrite, writeStarted)
		return SessionGrant{}, err
	}
	observeLoginTiming(ctx, LoginStageSessionWrite, writeStarted)
	pending.session.DeviceTrust = admission.TrustState
	if admission.TrustState == DeviceTrustEstablished {
		pending.session.TrustedDeviceID = admission.DeviceID
	}
	return SessionGrant{
		Session: pending.session, Credential: pending.issued.Credential,
		CSRFToken: pending.issued.CSRFToken, Device: &admission,
	}, nil
}

func (r *SessionRepository) reserveDeviceChallengePayload(
	ctx context.Context,
	candidate loginCandidate,
) (outbox.ProtectedPayloadReservation, error) {
	if r.devices == nil || candidate.role != RoleStudent {
		return outbox.ProtectedPayloadReservation{}, nil
	}
	return r.devices.reserveChallengePayload(ctx)
}

type pendingSession struct {
	session AuthenticatedSession
	issued  IssuedCredential
	now     time.Time
}

func (r *SessionRepository) prepareSession(
	candidate loginCandidate,
) (pendingSession, error) {
	sessionUUID, err := uuid.NewRandom()
	if err != nil {
		return pendingSession{}, fmt.Errorf("generating session family ID: %w", err)
	}
	sessionID := sessionUUID.String()
	issued, err := NewSessionCredentialForGeneration(r.csrfKey, sessionID, 1)
	if err != nil {
		return pendingSession{}, err
	}
	now := r.now().UTC()
	window := r.window(candidate.role)
	return pendingSession{issued: issued, now: now, session: AuthenticatedSession{
		AccountID:         candidate.accountID,
		SessionID:         sessionID,
		DisplayName:       candidate.displayName,
		Role:              candidate.role,
		CredentialState:   candidate.credentialState,
		Generation:        1,
		AuthenticatedAt:   now,
		IdleExpiresAt:     now.Add(window.IdleExpiry()),
		AbsoluteExpiresAt: now.Add(window.AbsoluteExpiry()),
	}}, nil
}

func (r *SessionRepository) persistSession(
	ctx context.Context,
	request LoginRequest,
	candidate loginCandidate,
	pending pendingSession,
	reservation outbox.ProtectedPayloadReservation,
) (DeviceAdmissionResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return DeviceAdmissionResult{}, fmt.Errorf("beginning login transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// This lock is also the device-policy serialization point. Two new browsers
	// authenticating at the same instant queue here, so the second one counts
	// the first one's device rather than racing it.
	if err := lockLoginCandidate(ctx, tx, candidate); err != nil {
		return DeviceAdmissionResult{}, err
	}
	admission, err := r.admitDevice(ctx, tx, request, candidate, reservation)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := insertSessionFamily(
		ctx, tx, pending.session, candidate.sessionEpoch, pending.now, admission,
	); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := insertSessionGeneration(
		ctx, tx, pending.session.SessionID, 1, pending.issued, pending.now,
	); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_CREATED", accountID: candidate.accountID,
		revision: candidate.revision, requestID: request.RequestID,
		evidence: map[string]any{
			"generation": 1, "role": candidate.role,
			"device_trust": string(admission.TrustState),
		},
	}); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceAdmissionResult{}, fmt.Errorf("committing login session: %w", err)
	}
	return admission, nil
}

// admitDevice decides what device authority this new family carries.
//
// With no device service wired, a Student family is created pending rather than
// trusted. That is the fail-closed direction: a misconfigured deployment loses
// protected learning until it is fixed, instead of quietly handing every
// Student an unlimited number of devices.
func (r *SessionRepository) admitDevice(
	ctx context.Context,
	tx pgx.Tx,
	request LoginRequest,
	candidate loginCandidate,
	reservation outbox.ProtectedPayloadReservation,
) (DeviceAdmissionResult, error) {
	if candidate.role != RoleStudent {
		return DeviceAdmissionResult{
			Admission: AdmitTrustedDevice, TrustState: DeviceTrustNotApplicable,
		}, nil
	}
	if r.devices == nil {
		return DeviceAdmissionResult{
			Admission: AdmitNewDeviceWithSlot, TrustState: DeviceTrustPending,
		}, nil
	}
	return r.devices.admitInTransaction(ctx, tx, DeviceAdmissionRequest{
		AccountID: candidate.accountID, Revision: candidate.revision,
		Role: candidate.role, Email: candidate.email, Locale: candidate.locale,
		PresentedDigest: request.DeviceCredentialDigest,
		UserAgent:       request.UserAgent, SourceAddress: request.SourceAddress,
		RequestID: request.RequestID, Reservation: reservation,
	})
}

func lockLoginCandidate(
	ctx context.Context,
	tx pgx.Tx,
	candidate loginCandidate,
) error {
	var status AccountStatus
	var role Role
	var credentialState CredentialState
	var epoch, revision int
	var verifiedAt *time.Time
	var passwordHash string
	err := tx.QueryRow(ctx,
		`SELECT a.status::text, a.role::text, c.state::text, a.session_epoch,
		        a.revision, a.email_verified_at, c.password_hash
		   FROM accounts a
		   JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.id = $1::uuid
		  FOR UPDATE OF a, c`,
		candidate.accountID,
	).Scan(&status, &role, &credentialState, &epoch, &revision, &verifiedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return fmt.Errorf("rechecking login authority: %w", err)
	}
	unchanged := status == StatusActive &&
		verifiedAt != nil &&
		role == candidate.role &&
		credentialState == candidate.credentialState &&
		epoch == candidate.sessionEpoch &&
		revision == candidate.revision &&
		passwordHash == candidate.passwordHash
	if !unchanged {
		return ErrAuthenticationFailed
	}
	return nil
}

func insertSessionFamily(
	ctx context.Context,
	tx pgx.Tx,
	session AuthenticatedSession,
	epoch int,
	now time.Time,
	admission DeviceAdmissionResult,
) error {
	// A new family never writes LEGACY_UNBOUND. That value exists only for rows
	// the migration found already present, and the schema check keeps
	// trusted_device_id and the state coherent with each other.
	deviceID := ""
	if admission.TrustState == DeviceTrustEstablished {
		deviceID = admission.DeviceID
	}
	trustState := admission.TrustState
	if !trustState.Valid() || trustState == DeviceTrustLegacyUnbound {
		trustState = DeviceTrustPending
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO sessions
		   (id, account_id, admitted_epoch, authenticated_at, last_activity_at,
		    idle_expires_at, absolute_expires_at, trusted_device_id, device_trust_state)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $4, $5, $6,
		         NULLIF($7, '')::uuid, $8::session_device_trust_state)`,
		session.SessionID,
		session.AccountID,
		epoch,
		now,
		session.IdleExpiresAt,
		session.AbsoluteExpiresAt,
		deviceID,
		string(trustState),
	)
	if err != nil {
		return fmt.Errorf("creating session family: %w", err)
	}
	return nil
}

func insertSessionGeneration(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	generation int,
	issued IssuedCredential,
	now time.Time,
) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO session_credentials
		   (session_id, generation, credential_digest, csrf_digest, issued_at)
		 VALUES ($1::uuid, $2, $3, $4, $5)`,
		sessionID, generation, issued.CredentialDigest, issued.CSRFDigest, now,
	)
	if err != nil {
		return fmt.Errorf("creating session credential generation: %w", err)
	}
	return nil
}

type sessionEvent struct {
	eventType string
	accountID string
	revision  int
	requestID string
	evidence  map[string]any
}

func appendSessionEvent(ctx context.Context, tx pgx.Tx, event sessionEvent) error {
	return appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: event.eventType,
		accountID: event.accountID,
		revision:  event.revision,
		requestID: event.requestID,
		evidence:  event.evidence,
	})
}

type sessionRecord struct {
	session             AuthenticatedSession
	accountStatus       AccountStatus
	sessionEpoch        int
	admittedEpoch       int
	sessionState        SessionState
	familyGeneration    int
	credentialRowState  string
	credentialDigest    string
	csrfDigest          string
	supersededAt        *time.Time
	staleUseCount       int
	revision            int
	deviceTrustState    string
	trustedDeviceID     *string
	trustedDeviceDigest *string
	deviceLive          bool
}

// SessionResolutionRequest carries both browser credentials used to resolve a
// Student session. The device credential never authenticates the Account; it
// only proves that a trusted Student family is still being presented from the
// browser to which it was bound.
type SessionResolutionRequest struct {
	CredentialDigest       string
	DeviceCredentialDigest string
	UseKind                CredentialUseKind
	RequestID              string
}

func (r *SessionRepository) Resolve(ctx context.Context, request SessionResolutionRequest) (SessionView, error) {
	record, err := readSessionRecord(ctx, r.pool, request.CredentialDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionView{}, ErrAuthenticationRequired
	}
	if err != nil {
		return SessionView{}, fmt.Errorf("resolving session credential: %w", err)
	}
	if record.credentialRowState == "SUPERSEDED" {
		return r.resolveSuperseded(ctx, request.CredentialDigest, request.UseKind, request.RequestID)
	}
	if err := record.usable(r.now().UTC()); err != nil {
		return SessionView{}, err
	}
	record.applyDeviceTrust(request.DeviceCredentialDigest)
	csrfToken, err := r.csrfToken(record)
	if err != nil {
		return SessionView{}, err
	}
	return SessionView{Session: record.session, CSRFToken: csrfToken}, nil
}

func (r *SessionRepository) resolveSuperseded(
	ctx context.Context,
	credentialDigest string,
	useKind CredentialUseKind,
	requestID string,
) (SessionView, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return SessionView{}, fmt.Errorf("beginning superseded session resolution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := loadSessionRecord(ctx, tx, credentialDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionView{}, ErrAuthenticationRequired
	}
	if err != nil {
		return SessionView{}, fmt.Errorf("locking superseded session credential: %w", err)
	}
	if record.credentialRowState != "SUPERSEDED" {
		return SessionView{}, ErrAuthenticationRequired
	}
	semanticErr := r.recordSupersededUse(ctx, tx, &record, useKind, requestID)
	return SessionView{}, commitSemantic(ctx, tx, semanticErr)
}

const sessionRecordQuery = `SELECT a.id::text, s.id::text, a.display_name, a.role::text,
		pc.state::text, c.generation, s.authenticated_at, s.reauthenticated_at,
		s.idle_expires_at, s.absolute_expires_at, a.status::text, a.session_epoch,
		s.admitted_epoch, s.state::text, s.current_generation, c.state::text,
		c.credential_digest, c.csrf_digest, c.superseded_at,
		c.stale_use_count, a.revision,
		s.device_trust_state::text, s.trusted_device_id::text, d.credential_digest,
		(d.id IS NOT NULL AND d.revoked_at IS NULL AND d.trusted_at IS NOT NULL)
	   FROM session_credentials c
	   JOIN sessions s ON s.id = c.session_id
	   JOIN accounts a ON a.id = s.account_id
	   JOIN password_credentials pc ON pc.account_id = a.id
	   LEFT JOIN identity_trusted_devices d ON d.id = s.trusted_device_id
	  WHERE c.credential_digest = $1`

func readSessionRecord(
	ctx context.Context,
	pool *pgxpool.Pool,
	credentialDigest string,
) (sessionRecord, error) {
	return scanSessionRecord(pool.QueryRow(ctx, sessionRecordQuery, credentialDigest))
}

func loadSessionRecord(
	ctx context.Context,
	tx pgx.Tx,
	credentialDigest string,
) (sessionRecord, error) {
	return scanSessionRecord(tx.QueryRow(
		ctx, sessionRecordQuery+` FOR UPDATE OF c, s, a, pc`, credentialDigest,
	))
}

func loadSessionGeneration(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	generation int,
) (sessionRecord, error) {
	return scanSessionRecord(tx.QueryRow(ctx,
		`SELECT a.id::text, s.id::text, a.display_name, a.role::text,
		        pc.state::text, c.generation, s.authenticated_at, s.reauthenticated_at,
		        s.idle_expires_at, s.absolute_expires_at, a.status::text, a.session_epoch,
		        s.admitted_epoch, s.state::text, s.current_generation, c.state::text,
		        c.credential_digest, c.csrf_digest, c.superseded_at,
		        c.stale_use_count, a.revision,
		        s.device_trust_state::text, s.trusted_device_id::text, d.credential_digest,
		        (d.id IS NOT NULL AND d.revoked_at IS NULL AND d.trusted_at IS NOT NULL)
		   FROM session_credentials c
		   JOIN sessions s ON s.id = c.session_id
		   JOIN accounts a ON a.id = s.account_id
		   JOIN password_credentials pc ON pc.account_id = a.id
		   LEFT JOIN identity_trusted_devices d ON d.id = s.trusted_device_id
		  WHERE s.id = $1::uuid AND c.generation = $2
		  FOR UPDATE OF c, s, a, pc`,
		sessionID,
		generation,
	))
}

func scanSessionRecord(row pgx.Row) (sessionRecord, error) {
	var record sessionRecord
	err := row.Scan(
		&record.session.AccountID,
		&record.session.SessionID,
		&record.session.DisplayName,
		&record.session.Role,
		&record.session.CredentialState,
		&record.session.Generation,
		&record.session.AuthenticatedAt,
		&record.session.ReauthenticatedAt,
		&record.session.IdleExpiresAt,
		&record.session.AbsoluteExpiresAt,
		&record.accountStatus,
		&record.sessionEpoch,
		&record.admittedEpoch,
		&record.sessionState,
		&record.familyGeneration,
		&record.credentialRowState,
		&record.credentialDigest,
		&record.csrfDigest,
		&record.supersededAt,
		&record.staleUseCount,
		&record.revision,
		&record.deviceTrustState,
		&record.trustedDeviceID,
		&record.trustedDeviceDigest,
		&record.deviceLive,
	)
	if err != nil {
		return record, err
	}
	return record, nil
}

// applyDeviceTrust turns the stored binding plus the device's current liveness
// into the trust state authorization actually sees.
//
// The row can say TRUSTED while the device it names has been revoked — that is
// precisely what happens between a revocation and this session's next request —
// and resolving that disagreement in favour of the row would make device
// revocation take effect only at expiry. It resolves to pending instead, which
// leaves the Student able to trust a device again and unable to reach protected
// learning in the meantime.
func (r *sessionRecord) applyDeviceTrust(presentedDigest string) {
	state := SessionDeviceTrust(r.deviceTrustState)
	if !state.Valid() {
		state = DeviceTrustPending
	}
	deviceCredentialMatches := r.trustedDeviceDigest != nil && presentedDigest != "" &&
		OpaqueDigestEqual(*r.trustedDeviceDigest, presentedDigest)
	if state == DeviceTrustEstablished && (!r.deviceLive || !deviceCredentialMatches) {
		r.session.DeviceTrust = DeviceTrustPending
		r.session.TrustedDeviceID = ""
		return
	}
	r.session.DeviceTrust = state
	if r.trustedDeviceID != nil && state == DeviceTrustEstablished {
		r.session.TrustedDeviceID = *r.trustedDeviceID
	}
}

// RecheckForMutation locks and revalidates the Account, family, and exact
// generation inside the caller's domain transaction.
func (r *SessionRepository) RecheckForMutation(
	ctx context.Context,
	tx pgx.Tx,
	authenticated AuthenticatedSession,
	csrfDigest string,
) error {
	record, err := loadSessionGeneration(
		ctx, tx, authenticated.SessionID, authenticated.Generation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthenticationRequired
	}
	if err != nil {
		return fmt.Errorf("rechecking session mutation authority: %w", err)
	}
	if err := record.usable(r.now().UTC()); err != nil {
		return err
	}
	unchanged := record.session.AccountID == authenticated.AccountID &&
		record.session.Role == authenticated.Role &&
		record.session.CredentialState == authenticated.CredentialState
	if !unchanged {
		return ErrAuthenticationRequired
	}
	if !digestEqual(csrfDigest, record.csrfDigest) {
		return ErrSessionCSRFFailed
	}
	return nil
}

func (r sessionRecord) usable(now time.Time) error {
	if r.accountStatus != StatusActive ||
		r.sessionEpoch != r.admittedEpoch ||
		r.sessionState != SessionActive ||
		r.familyGeneration != r.session.Generation ||
		r.credentialRowState != "CURRENT" {
		return ErrAuthenticationRequired
	}
	if r.session.Generation < 1 {
		return ErrAuthenticationRequired
	}
	family := Session{
		AdmittedEpoch:     r.admittedEpoch,
		State:             r.sessionState,
		IdleExpiresAt:     r.session.IdleExpiresAt,
		AbsoluteExpiresAt: r.session.AbsoluteExpiresAt,
	}
	if err := family.Usable(r.sessionEpoch, now); err != nil {
		return ErrAuthenticationRequired
	}
	return nil
}

func (r *SessionRepository) csrfToken(record sessionRecord) (config.Secret, error) {
	csrfPlaintext, err := deriveSessionCSRFToken(
		r.csrfKey,
		record.session.SessionID,
		record.session.Generation,
		record.credentialDigest,
	)
	if err != nil {
		return config.Secret{}, err
	}
	if !digestEqual(DigestToken(csrfPlaintext), record.csrfDigest) {
		return config.Secret{}, errors.New("stored session CSRF digest does not match generation facts")
	}
	return config.NewSecret(csrfPlaintext), nil
}

func digestEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (r *SessionRepository) recordSupersededUse(
	ctx context.Context,
	tx pgx.Tx,
	record *sessionRecord,
	useKind CredentialUseKind,
	requestID string,
) error {
	if record.supersededAt == nil {
		return errors.New("superseded session credential has no supersession time")
	}
	now := r.now().UTC()
	decision := ClassifySupersededCredentialUse(SupersededCredentialUse{
		Kind: useKind, SupersededAt: *record.supersededAt,
		PriorStaleUseCount: record.staleUseCount,
	}, now, r.settings.StaleUseWindow())
	if err := incrementStaleEvidence(ctx, tx, *record, now); err != nil {
		return err
	}
	if decision == StaleUseRejectReplaced {
		return recordReplacementEvent(ctx, tx, *record, requestID)
	}
	return revokeReusedSession(ctx, tx, *record, requestID, now)
}

func recordReplacementEvent(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	requestID string,
) error {
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_REPLACED_PRESENTED", accountID: record.session.AccountID,
		revision: record.revision, requestID: requestID,
		evidence: map[string]any{"generation": record.session.Generation},
	}); err != nil {
		return err
	}
	return ErrSessionReplaced
}

func revokeReusedSession(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	requestID string,
	now time.Time,
) error {
	if err := revokeSessionFamily(
		ctx, tx, record.session.SessionID, RevokedByReuseDetected, now,
	); err != nil {
		return err
	}
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_REUSE_DETECTED", accountID: record.session.AccountID,
		revision: record.revision, requestID: requestID,
		evidence: map[string]any{"generation": record.session.Generation},
	}); err != nil {
		return err
	}
	return ErrSessionReuseDetected
}

func incrementStaleEvidence(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE session_credentials
		    SET stale_use_count = stale_use_count + 1,
		        first_stale_use_at = COALESCE(first_stale_use_at, $3),
		        last_stale_use_at = $3
		  WHERE session_id = $1::uuid AND generation = $2 AND state = 'SUPERSEDED'`,
		record.session.SessionID, record.session.Generation, now,
	)
	if err != nil {
		return fmt.Errorf("recording stale session credential use: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("stale session credential changed during classification")
	}
	return nil
}

func commitSemantic(ctx context.Context, tx pgx.Tx, semanticErr error) error {
	if semanticErr == nil {
		return errors.New("session semantic outcome is required")
	}
	if !errors.Is(semanticErr, ErrSessionReplaced) &&
		!errors.Is(semanticErr, ErrSessionReuseDetected) {
		return semanticErr
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing session security response: %w", err)
	}
	return semanticErr
}

func (r *SessionRepository) Renew(
	ctx context.Context,
	request SessionMutation,
) (SessionGrant, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return SessionGrant{}, fmt.Errorf("beginning session renewal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := loadSessionRecord(ctx, tx, request.CredentialDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionGrant{}, ErrAuthenticationRequired
	}
	if err != nil {
		return SessionGrant{}, fmt.Errorf("loading session for renewal: %w", err)
	}
	if record.credentialRowState == "SUPERSEDED" {
		semanticErr := r.recordSupersededUse(
			ctx, tx, &record, UseRenewal, request.RequestID,
		)
		return SessionGrant{}, commitSemantic(ctx, tx, semanticErr)
	}
	now := r.now().UTC()
	if err := record.usable(now); err != nil {
		return SessionGrant{}, err
	}
	if !digestEqual(request.CSRFDigest, record.csrfDigest) {
		return SessionGrant{}, ErrSessionCSRFFailed
	}
	return r.renewCurrent(ctx, tx, record, request.RequestID, now)
}

type pendingRenewal struct {
	session AuthenticatedSession
	issued  IssuedCredential
	now     time.Time
}

func (r *SessionRepository) renewCurrent(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	requestID string,
	now time.Time,
) (SessionGrant, error) {
	nextGeneration := record.session.Generation + 1
	issued, err := NewSessionCredentialForGeneration(
		r.csrfKey, record.session.SessionID, nextGeneration,
	)
	if err != nil {
		return SessionGrant{}, err
	}
	renewedSession := record.session
	renewedSession.Generation = nextGeneration
	renewedSession.IdleExpiresAt = minTime(
		now.Add(r.window(record.session.Role).IdleExpiry()),
		record.session.AbsoluteExpiresAt,
	)
	pending := pendingRenewal{session: renewedSession, issued: issued, now: now}
	if err := persistRenewal(ctx, tx, record, pending, requestID); err != nil {
		return SessionGrant{}, err
	}
	return SessionGrant{
		Session: pending.session, Credential: pending.issued.Credential,
		CSRFToken: pending.issued.CSRFToken,
	}, nil
}

func persistRenewal(
	ctx context.Context,
	tx pgx.Tx,
	previous sessionRecord,
	pending pendingRenewal,
	requestID string,
) error {
	if err := supersedeCurrentGeneration(
		ctx, tx, previous, pending.session.Generation, pending.now,
	); err != nil {
		return err
	}
	if err := insertSessionGeneration(
		ctx, tx, previous.session.SessionID, pending.session.Generation,
		pending.issued, pending.now,
	); err != nil {
		return err
	}
	if err := advanceSessionFamily(ctx, tx, pending.session, pending.now); err != nil {
		return err
	}
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_RENEWED", accountID: previous.session.AccountID,
		revision: previous.revision, requestID: requestID,
		evidence: map[string]any{"generation": pending.session.Generation},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing session renewal: %w", err)
	}
	return nil
}

func supersedeCurrentGeneration(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	nextGeneration int,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE session_credentials
		    SET state = 'SUPERSEDED', superseded_at = $4, replaced_by_generation = $3
		  WHERE session_id = $1::uuid AND generation = $2 AND state = 'CURRENT'`,
		record.session.SessionID, record.session.Generation, nextGeneration, now,
	)
	if err != nil {
		return fmt.Errorf("superseding session generation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrSessionReuseDetected
	}
	return nil
}

func advanceSessionFamily(
	ctx context.Context,
	tx pgx.Tx,
	session AuthenticatedSession,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET current_generation = $2, last_activity_at = $3,
		        idle_expires_at = $4, updated_at = $3
		  WHERE id = $1::uuid AND state = 'ACTIVE'`,
		session.SessionID, session.Generation, now, session.IdleExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("advancing session family generation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrAuthenticationRequired
	}
	return nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (r *SessionRepository) Logout(
	ctx context.Context,
	request SessionMutation,
) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("beginning session logout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := loadSessionRecord(ctx, tx, request.CredentialDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthenticationRequired
	}
	if err != nil {
		return fmt.Errorf("loading session for logout: %w", err)
	}
	if record.credentialRowState == "SUPERSEDED" {
		semanticErr := r.recordSupersededUse(
			ctx, tx, &record, UseStateChanging, request.RequestID,
		)
		return commitSemantic(ctx, tx, semanticErr)
	}
	now := r.now().UTC()
	if err := record.usable(now); err != nil {
		return err
	}
	if !digestEqual(request.CSRFDigest, record.csrfDigest) {
		return ErrSessionCSRFFailed
	}
	return logoutCurrent(ctx, tx, record, request.RequestID, now)
}

func logoutCurrent(
	ctx context.Context,
	tx pgx.Tx,
	record sessionRecord,
	requestID string,
	now time.Time,
) error {
	if err := revokeSessionFamily(
		ctx, tx, record.session.SessionID, RevokedByLogout, now,
	); err != nil {
		return err
	}
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_LOGGED_OUT", accountID: record.session.AccountID,
		revision: record.revision, requestID: requestID,
		evidence: map[string]any{"generation": record.session.Generation},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing session logout: %w", err)
	}
	return nil
}

func revokeSessionFamily(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	reason RevocationReason,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET state = 'REVOKED', revoked_at = $3,
		        revocation_reason = $2, updated_at = $3
		  WHERE id = $1::uuid AND state = 'ACTIVE'`,
		sessionID, reason, now,
	)
	if err != nil {
		return fmt.Errorf("revoking session family: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrAuthenticationRequired
	}
	return nil
}

func (r *SessionRepository) window(role Role) config.SessionWindow {
	switch role {
	case RoleInstructor:
		return r.settings.Instructor()
	case RoleAdmin:
		return r.settings.Admin()
	default:
		return r.settings.Student()
	}
}

// IssueSessionInTransaction mints one ordinary session family for an Account
// whose authority this transaction has already established.
//
// It exists so email verification can activate an Account and authenticate it
// atomically. The alternatives were both worse: committing the activation and
// then creating a session in a second transaction leaves a verified Student
// unauthenticated whenever the second one fails, and carrying a
// pre-verification session forward would mean a session existed before the
// proof that justifies it.
//
// This is not a weaker session. It uses the same family record, the same
// generation-bound credential, the same CSRF derivation, the same role window,
// and the same security-event trail as Login. What differs is only the
// evidence that preceded it — a consumed one-time code instead of a password —
// and the caller is responsible for having verified that inside this same
// transaction before calling.
//
// The Account row is re-read here under the caller's lock rather than trusted
// from a parameter: the session window and the admitted epoch must come from
// the row this transaction will commit, not from a value read before it.
// IssueSessionInTransaction mints a family inside a caller's transaction.
//
// The device decision is supplied rather than made here: the only caller is
// email verification, which has just proven the Student's mailbox with a code
// and can therefore trust the browser outright instead of mailing a second one
// for the same mailbox seconds later.
func (r *SessionRepository) IssueSessionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	requestID string,
	device *DeviceAdmissionResult,
) (SessionGrant, error) {
	if r == nil || tx == nil {
		return SessionGrant{}, errors.New("session repository and transaction are required")
	}
	var candidate loginCandidate
	err := tx.QueryRow(ctx,
		`SELECT a.id::text, a.display_name, a.role::text, a.status::text,
		        c.state::text, a.session_epoch, a.revision, a.email_verified_at
		   FROM accounts a
		   JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.id = $1::uuid`,
		accountID,
	).Scan(
		&candidate.accountID, &candidate.displayName, &candidate.role,
		&candidate.status, &candidate.credentialState, &candidate.sessionEpoch,
		&candidate.revision, &candidate.verifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionGrant{}, ErrAuthenticationFailed
	}
	if err != nil {
		return SessionGrant{}, fmt.Errorf("reading session subject: %w", err)
	}
	// The same predicate Login applies. An Account that is not ACTIVE and
	// verified in this transaction does not get a session out of it, whatever
	// the caller believed it had just done.
	if !candidate.loginAllowed() {
		return SessionGrant{}, ErrAuthenticationFailed
	}
	pending, err := r.prepareSession(candidate)
	if err != nil {
		return SessionGrant{}, err
	}
	admission := DeviceAdmissionResult{
		Admission: AdmitTrustedDevice, TrustState: DeviceTrustNotApplicable,
	}
	if device != nil {
		admission = *device
	} else if candidate.role == RoleStudent {
		// A Student session minted here without a device decision fails closed,
		// for the same reason a login without device policy does.
		admission = DeviceAdmissionResult{
			Admission: AdmitNewDeviceWithSlot, TrustState: DeviceTrustPending,
		}
	}
	pending.session.DeviceTrust = admission.TrustState
	if admission.TrustState == DeviceTrustEstablished {
		pending.session.TrustedDeviceID = admission.DeviceID
	}
	if err := insertSessionFamily(
		ctx, tx, pending.session, candidate.sessionEpoch, pending.now, admission,
	); err != nil {
		return SessionGrant{}, err
	}
	if err := insertSessionGeneration(
		ctx, tx, pending.session.SessionID, 1, pending.issued, pending.now,
	); err != nil {
		return SessionGrant{}, err
	}
	if err := appendSessionEvent(ctx, tx, sessionEvent{
		eventType: "SESSION_CREATED", accountID: candidate.accountID,
		revision: candidate.revision, requestID: requestID,
		evidence: map[string]any{
			"generation": 1, "role": candidate.role,
			// Recorded so the trail distinguishes a password login from a
			// session minted by proving a verification code.
			"authentication_method": "EMAIL_VERIFICATION_OTP",
		},
	}); err != nil {
		return SessionGrant{}, err
	}
	return SessionGrant{
		Session: pending.session, Credential: pending.issued.Credential,
		CSRFToken: pending.issued.CSRFToken, Device: &admission,
	}, nil
}
