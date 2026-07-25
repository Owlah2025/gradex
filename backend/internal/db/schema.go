package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The schema versions this build can serve.
//
// A range rather than a single number because §11.1 stages schema change as
// expand → migrate → cutover → observe → contract: during a rollout the old
// and new application versions both run against the expanded schema, so an
// exact-match requirement would make every migration an outage.
//
// Raise MinSchemaVersion only in the contract step, once no running instance
// depends on the older shape. Version 1 stays supported because 0002 and 0003
// only add tables — nothing a version-1 build reads has changed shape.
const (
	MinSchemaVersion = 1
	MaxSchemaVersion = 3
)

// schemaMigrationsTable is golang-migrate's bookkeeping table. cmd/migrate
// writes it; the application only reads it.
const schemaMigrationsTable = "schema_migrations"

var (
	// ErrSchemaMissing means migrations have never run against this database.
	ErrSchemaMissing = errors.New("schema is not initialized")
	// ErrSchemaDirty means a migration failed partway. The database is in an
	// unknown state and an operator must resolve it; serving traffic against
	// it risks acting on a half-applied shape.
	ErrSchemaDirty = errors.New("schema is dirty from a failed migration")
	// ErrSchemaIncompatible means the database is outside the range this build
	// supports.
	ErrSchemaIncompatible = errors.New("schema version is not supported by this build")
)

// SchemaState is what the bookkeeping table currently reports.
type SchemaState struct {
	Version int64
	Dirty   bool
}

// Ping proves the pool can execute a statement, not merely that a TCP
// connection opened. It is deliberately the cheapest possible query: a probe
// must never become load.
func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("executing readiness query: %w", err)
	}
	return nil
}

// ReadSchemaState reads the migration marker through the normal application
// pool.
func ReadSchemaState(ctx context.Context, pool *pgxpool.Pool) (SchemaState, error) {
	var s SchemaState
	err := pool.QueryRow(ctx,
		"SELECT version, dirty FROM "+schemaMigrationsTable+" ORDER BY version DESC LIMIT 1",
	).Scan(&s.Version, &s.Dirty)

	if errors.Is(err, pgx.ErrNoRows) {
		return SchemaState{}, ErrSchemaMissing
	}
	if err != nil {
		// An absent table is the same operational situation as an empty one:
		// this database has not been migrated.
		return SchemaState{}, fmt.Errorf("%w: %v", ErrSchemaMissing, err)
	}
	return s, nil
}

// CheckSchema reports whether this build may serve traffic against the
// database's current schema.
//
// It fails closed: a missing marker, a dirty marker, or a version outside the
// supported range all mean not ready. Readiness is the right place to enforce
// this — the process starts, so an operator can reach its logs, but it
// receives no traffic until the schema is one this build understands.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("%w: version %d", ErrSchemaDirty, state.Version)
	}
	if state.Version < MinSchemaVersion || state.Version > MaxSchemaVersion {
		return fmt.Errorf("%w: found %d, this build supports %d..%d",
			ErrSchemaIncompatible, state.Version, MinSchemaVersion, MaxSchemaVersion)
	}
	return nil
}
