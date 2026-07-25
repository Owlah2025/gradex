package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPrincipalNotFound means no Account matched the authenticated identifier.
//
// This is a denial, not a fault. It is kept distinct from a database error so
// the HTTP layer can tell "this caller has no Account" from "the database is
// unreachable" in its logs — while returning the same uniform refusal for both.
var ErrPrincipalNotFound = errors.New("no Account for the authenticated identifier")

// PrincipalResolver turns an authenticated identifier into the Principal
// authorization reads.
//
// It is an interface so the HTTP layer depends on the capability, not on a
// database pool, and so tests can drive principal shapes that are awkward to
// create as real rows.
type PrincipalResolver interface {
	ResolvePrincipal(ctx context.Context, accountID string) (Principal, error)
}

// DBPrincipalResolver reads the Principal from PostgreSQL on every request.
//
// Every request, deliberately. Caching it would reintroduce exactly the
// staleness that keeping capability off the session avoids: a cached principal
// keeps granting access after a suspension or a completed password change. If
// this ever becomes a measured bottleneck, the fix is a short-TTL cache with
// explicit invalidation on the state transitions, decided then — not an
// unbounded cache added now on speculation.
type DBPrincipalResolver struct {
	pool *pgxpool.Pool
}

func NewDBPrincipalResolver(pool *pgxpool.Pool) *DBPrincipalResolver {
	return &DBPrincipalResolver{pool: pool}
}

func (r *DBPrincipalResolver) ResolvePrincipal(ctx context.Context, accountID string) (Principal, error) {
	var p Principal
	p.AccountID = accountID

	// LEFT JOIN, not JOIN: an Account with no password credential must still
	// resolve to a principal rather than silently vanishing into
	// ErrPrincipalNotFound, which would misreport a data problem as an unknown
	// caller. A missing credential is treated as CHANGE_REQUIRED below —
	// the restrictive reading, since an Account that cannot prove a password is
	// not an Account that should hold Admin authority.
	var credentialState *string
	err := r.pool.QueryRow(ctx,
		`SELECT a.role::text, a.status::text, c.state::text
		   FROM accounts a
		   LEFT JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.id = $1::uuid`,
		accountID,
	).Scan(&p.Role, &p.Status, &credentialState)

	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrPrincipalNotFound
	}
	if err != nil {
		// A malformed identifier reaches the driver as a uuid cast failure.
		// That is an unknown caller, not an outage, and reporting it as a fault
		// would turn a probing request into a false infrastructure alert.
		if isInvalidUUID(err) {
			return Principal{}, ErrPrincipalNotFound
		}
		return Principal{}, fmt.Errorf("resolving principal: %w", err)
	}

	if credentialState == nil {
		p.CredentialState = CredentialChangeRequired
	} else {
		p.CredentialState = CredentialState(*credentialState)
	}
	return p, nil
}

// isInvalidUUID recognizes the driver's response to a value that cannot be cast
// to uuid.
func isInvalidUUID(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		// 22P02 invalid_text_representation.
		return pgErr.SQLState() == "22P02"
	}
	return false
}
