// Command migrate applies Gradex's database migrations.
//
// It exists so migrations need no globally installed binary: `go run
// ./cmd/migrate up` works from a clean checkout and in CI with nothing
// pre-provisioned. It reads the same typed configuration and secret boundary
// as the API and worker, so a migration cannot run against a database the
// application itself could not be configured to reach.
//
// It is a controlled one-off release command in its own entrypoint, matching
// the execution class the architecture assigns to schema migrations. The
// application never invokes it: production startup checks schema compatibility
// through readiness and refuses traffic on a mismatch, rather than silently
// migrating whatever it finds.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsSource is relative to the backend module root, which is where
// `go run ./cmd/migrate` executes from.
const migrationsSource = "file://internal/db/migrations"

// lockTimeout bounds how long this command waits for the migration advisory
// lock another instance may hold. Without a bound, a stuck deploy blocks
// forever instead of failing the release.
const lockTimeout = 30 * time.Second

// maxVersionCommand names the subcommand that prints db.MaxSchemaVersion.
//
// It exists so the migration contract has exactly one authority. CI previously
// asserted the post-migration schema version as a hardcoded literal kept in
// step with the Go constant by comment alone; migration 0006 raised the
// constant, the literal was not updated, and the Migrations job failed on an
// otherwise sound slice. Reading the value from the binary makes that class of
// drift unrepresentable rather than merely discouraged.
const maxVersionCommand = "max-version"

// schemaRangeCommand reports the release-level database range required to run
// every application role in this image. It is deliberately separate from the
// worker's narrower media floor: release selection starts API, worker and
// frontend together, so its minimum must be the API's direct-grant floor.
const schemaRangeCommand = "schema-range"

const (
	upSchema45Command = "up-schema-45"
	upSchema46Command = "up-schema-46"
)

func main() {
	// The DSN is scrubbed from every message: it carries the database
	// password, and a migration failure is exactly when output gets pasted
	// into a ticket. Driver and library errors are not written with that in
	// mind, so this is enforced at the one place everything exits through
	// rather than trusted to each error site.
	scrub := newScrubber()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %s\n", scrub(err.Error()))
		os.Exit(1)
	}
}

// newScrubber captures the credential-bearing settings once, then removes them
// from any string on its way to output. It reads the environment directly
// because it must work even when configuration loading is what failed.
func newScrubber() func(string) string {
	var secrets []string
	for _, key := range []string{"DATABASE_URL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			secrets = append(secrets, v)
			// Also scrub the password on its own: drivers frequently
			// reassemble a connection string rather than echoing the original.
			if u, err := url.Parse(v); err == nil && u.User != nil {
				if pw, set := u.User.Password(); set && pw != "" {
					secrets = append(secrets, pw)
				}
			}
		}
	}
	return func(s string) string {
		for _, secret := range secrets {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
		return s
	}
}

// rollbackSchema41Command is the one supervised production-capable downgrade in
// this binary, and it exists for exactly one migration.
//
// The generic `down` path stays prohibited in production and no flag is added
// to change that — see the D-103 procedure in docs/launch/RUNBOOK.md, which
// states the rule this command is deliberately narrower than. What that rule
// refuses is an arbitrary production downgrade: a step count, a target version,
// a sequence of older down migrations. This command accepts none of those. It
// means one thing and can mean nothing else: the database is at 41, exactly
// 0041 is reverted, and the result is 40.
//
// It is an emergency path for the 3C-A foundation release, available only while
// the schema-40 rollback floor is open — that is, while every processing
// attempt is still FULL. Once a later phase records the first ENHANCEMENT or
// FINALIZATION attempt, this command refuses like everything else.
const rollbackSchema41Command = "rollback-schema-41"

// rollbackSchema41Confirmation is the acknowledgement value required in
// production. It names the exact transition rather than authorizing "a
// downgrade" generically, so it cannot be reused, copied into an unrelated
// runbook, or made to authorize any other schema movement.
const rollbackSchema41Confirmation = "schema-41-to-40"

// rollbackSchema42Command is the supervised production downgrade for the 3C-B
// manual enhancement recovery release, and — like its schema-41 predecessor — it
// exists for exactly one migration.
//
// It means one thing and can mean nothing else: the database is clean at 42,
// exactly 0042 is reverted, and the result is a clean 41. It takes no target
// version and no step count, it cannot reach 40, and it ends at 41. Continuing
// on to 41 -> 40 is not a rollback this command performs or offers; that is a
// separate supervised command with its own, stricter, evidence rule.
//
// Adding it does not widen the generic `down` path, which stays prohibited in
// production with no flag to change that.
const rollbackSchema42Command = "rollback-schema-42"

// rollbackSchema42Confirmation names its exact transition, so it cannot be
// copied from a schema-41 runbook and cannot authorize any other movement.
const rollbackSchema42Confirmation = "schema-42-to-41"

func usage() error {
	return errors.New("usage: migrate <up|down|version|max-version|schema-range|up-schema-45|up-schema-46|" +
		rollbackSchema41Command + "|" + rollbackSchema42Command + "> [steps]")
}

// requireProductionAcknowledgement applies the acknowledgement rule shared by
// every supervised downgrade: required, and exact, in production; refused
// outside it, so the flag can never become a habit that is passed everywhere and
// therefore means nothing.
func requireProductionAcknowledgement(cfg *config.Config, command, want, got string) error {
	if cfg.Environment().IsProduction() {
		if got != want {
			return fmt.Errorf("APP_ENV=production requires -confirm-production=%s for %s", want, command)
		}
		return nil
	}
	if got != "" {
		return fmt.Errorf("-confirm-production was passed but APP_ENV=%s", cfg.Environment())
	}
	return nil
}

// requireCleanSchemaAt answers "dirty?" before "which version?", because a dirty
// marker means the recorded version may not describe the database at all. It
// runs before golang-migrate is asked to do anything, so a refusal leaves the
// marker exactly as it was found.
func requireCleanSchemaAt(m *migrate.Migrate, command string, want, resulting int) error {
	current, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("reading current schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("schema is dirty at version %d; %s will not act on a half-applied schema, resolve it manually first",
			current, command)
	}
	if current != uint(want) {
		return fmt.Errorf("schema is at version %d; %s reverts only %d to %d and is not authorized to cross any other version",
			current, command, want, resulting)
	}
	return nil
}

func run() error {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		return usage()
	}

	// max-version reports the schema version this build targets. It answers a
	// question about the compiled binary, not about any database, so it runs
	// before configuration loading: CI must be able to ask what version to
	// expect without a reachable database or a valid DSN.
	if args[0] == maxVersionCommand {
		return maxVersion()
	}
	if args[0] == schemaRangeCommand {
		return schemaRange()
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	m, err := migrate.New(migrationsSource, cfg.DatabaseURL().Expose())
	if err != nil {
		return fmt.Errorf("opening migration source: %w", err)
	}
	m.LockTimeout = lockTimeout
	defer func() {
		// Both returned errors are closing errors, not migration errors; the
		// command's exit status already reflects the migration outcome.
		sourceErr, dbErr := m.Close()
		if sourceErr != nil || dbErr != nil {
			fmt.Fprintln(os.Stderr, "migrate: error closing migration handles")
		}
	}()

	switch args[0] {
	case "up":
		return up(m)
	case upSchema45Command:
		return upExactly(m, upSchema45Command, db.DirectPurchaseAccessGrantSchemaVersion, db.AutoEnhancementRecoverySchemaVersion)
	case upSchema46Command:
		return upExactly(m, upSchema46Command, db.AutoEnhancementRecoverySchemaVersion, db.LessonPublicPreviewSchemaVersion)
	case "down":
		return down(m, cfg, args[1:])
	case rollbackSchema41Command:
		return rollbackSchema41(m, cfg, args[1:])
	case rollbackSchema42Command:
		return rollbackSchema42(m, cfg, args[1:])
	case "version":
		return version(m)
	default:
		return usage()
	}
}

func schemaRange() error {
	// The API is the highest required producer in a complete application tier.
	// This is compiled from the same constants readiness uses; manifests may bind
	// its output, but must not invent a floor in mutable host configuration.
	fmt.Printf("%d %d\n", db.DirectPurchaseAccessGrantSchemaVersion, db.MaxSchemaVersion)
	return nil
}

func upExactly(m *migrate.Migrate, command string, from, to int) error {
	if err := requireCleanSchemaAt(m, command, from, to); err != nil {
		return err
	}
	if err := m.Migrate(uint(to)); err != nil {
		after, dirty, versionErr := m.Version()
		if versionErr != nil {
			return fmt.Errorf("%s failed: %w (resulting schema state could not be read)", command, err)
		}
		return fmt.Errorf("%s failed: %w (schema is now version=%d dirty=%t; no automatic repair was attempted)", command, err, after, dirty)
	}
	return report(m, command)
}

func up(m *migrate.Migrate) error {
	if err := requireClean(m); err != nil {
		return err
	}
	// ErrNoChange means the database is already at the target version, which
	// is a success for a release step that may run more than once.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return report(m, "up")
}

// down is a development and test affordance. It refuses to run in production
// because an automated destructive down migration is how a release becomes
// data loss; recovering a production schema is a deliberate, supervised
// operation with a backup taken first.
func down(m *migrate.Migrate, cfg *config.Config, args []string) error {
	if cfg.Environment().IsProduction() {
		return errors.New("down migrations are not permitted when APP_ENV=production")
	}
	if err := requireClean(m); err != nil {
		return err
	}

	steps := 1
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fmt.Errorf("down expects a positive step count, got %q", args[0])
		}
		steps = n
	}
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("reading current schema version: %w", err)
	}
	if !dirty && version >= db.ManualPurchaseRequestsSchemaVersion && steps > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
		defer cancel()
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL().Expose())
		if err != nil {
			return fmt.Errorf("opening rollback preflight connection: %w", err)
		}
		defer pool.Close()
		if version >= db.ActiveProcessingKindSchemaVersion && int64(version)-int64(steps) < db.ActiveProcessingKindSchemaVersion {
			if err := db.CheckActiveProcessingKindRollbackSafety(ctx, pool); err != nil {
				return err
			}
		}
		if version >= db.CourseThumbnailSchemaVersion && int64(version)-int64(steps) < db.CourseThumbnailSchemaVersion {
			if err := db.CheckThumbnailRollbackSafety(ctx, pool); err != nil {
				return err
			}
		}
		// Only when this rollback would actually cross 41 -> 40. A down that
		// stays at or above 41 leaves attempt_kind in place and has nothing to
		// misrepresent, and an up or version command never reaches here at all.
		if version >= db.EnhancementRecoveryFoundationSchemaVersion &&
			int64(version)-int64(steps) < db.EnhancementRecoveryFoundationSchemaVersion {
			if err := db.CheckEnhancementRecoveryRollbackSafety(ctx, pool); err != nil {
				return err
			}
		}
		if err := db.CheckManualPurchaseRollbackSafety(ctx, pool); err != nil {
			return err
		}
	}

	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("reverting migrations: %w", err)
	}
	return report(m, "down")
}

// rollbackSchema41 performs the supervised 41 -> 40 foundation rollback.
//
// Every refusal below happens BEFORE golang-migrate is asked to do anything, so
// a refused rollback leaves the schema marker exactly as it found it: version
// 41, not dirty. That is the whole point of doing the work here rather than
// letting the down migration's own SQL guard raise mid-step.
//
// The SQL guard in 0041_enhancement_recovery_foundation.down.sql is retained as
// defence in depth against a runner that bypasses this command entirely.
func rollbackSchema41(m *migrate.Migrate, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet(rollbackSchema41Command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	confirm := fs.String("confirm-production", "",
		"required acknowledgement when APP_ENV=production; must be exactly "+rollbackSchema41Confirmation)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s takes no positional arguments; it reverts exactly schema %d to %d",
			rollbackSchema41Command, db.EnhancementRecoveryFoundationSchemaVersion, db.MediaPlayableFoundationSchemaVersion)
	}

	// The acknowledgement follows the declared environment exactly, in the shape
	// cmd/bootstrap-admin established.
	if err := requireProductionAcknowledgement(cfg, fmt.Sprintf("the supervised schema %d to %d rollback",
		db.EnhancementRecoveryFoundationSchemaVersion, db.MediaPlayableFoundationSchemaVersion),
		rollbackSchema41Confirmation, *confirm); err != nil {
		return err
	}
	if err := requireCleanSchemaAt(m, rollbackSchema41Command,
		db.EnhancementRecoveryFoundationSchemaVersion, db.MediaPlayableFoundationSchemaVersion); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		return fmt.Errorf("opening supervised rollback preflight connection: %w", err)
	}
	defer pool.Close()
	// Same evidence rule the generic down path applies when it crosses 41 -> 40.
	// Sharing it means the supervised command cannot drift into a weaker one.
	if err := db.CheckEnhancementRecoveryRollbackSafety(ctx, pool); err != nil {
		return err
	}
	// A safety gate against an incomplete quiesce, not synchronization; the
	// operational contract still requires the API and worker to be stopped.
	if err := db.CheckNoActiveMediaClaims(ctx, pool); err != nil {
		return err
	}

	return stepDownOnce(m, rollbackSchema41Command, db.MediaPlayableFoundationSchemaVersion)
}

// stepDownOnce executes the single reversal a supervised rollback authorizes and
// then proves where it landed.
//
// It never forces a version, clears a dirty marker, retries, or continues to a
// further down migration. If the step fails the marker state is genuinely
// unknown to this process — the down SQL may have refused before touching
// anything, or golang-migrate may have marked the version dirty first — so it
// reports what the database actually says instead of claiming it is clean, and
// leaves repair to an operator.
func stepDownOnce(m *migrate.Migrate, command string, resulting int) error {
	if err := m.Steps(-1); err != nil {
		after, afterDirty, versionErr := m.Version()
		if versionErr != nil {
			return fmt.Errorf("supervised schema rollback failed: %w (the resulting schema state could not be read: %v)", err, versionErr)
		}
		return fmt.Errorf("supervised schema rollback failed: %w (schema is now version=%d dirty=%t; no automatic repair was attempted)",
			err, after, afterDirty)
	}
	after, afterDirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("reading resulting schema version: %w", err)
	}
	if after != uint(resulting) || afterDirty {
		return fmt.Errorf("supervised schema rollback did not land on a clean version %d: version=%d dirty=%t; do not start the baseline application",
			resulting, after, afterDirty)
	}
	return report(m, command)
}

// rollbackSchema42 performs the supervised 42 -> 41 rollback of the 3C-B manual
// enhancement recovery release.
//
// Every refusal happens BEFORE golang-migrate is asked to do anything, so a
// refused rollback leaves the marker exactly as it found it: version 42, not
// dirty. The SQL guard inside 0042_active_processing_attempt_kind.down.sql is
// retained as defence in depth against a runner that bypasses this command.
//
// Three preconditions are asked of the database, in this order, and each is a
// hard gate rather than a warning:
//
//  1. no active processing operation, because an in-flight claim is what needs
//     the schema-42 kind column;
//  2. no claimed media work at all, which catches an incomplete quiesce;
//  3. no undispatched media.enhancement_requested outbox event, because the
//     schema-41 dispatcher aborts its batch on that event type and would then
//     block every later media outbox event permanently.
//
// The third gate covers only PostgreSQL. A dispatched intent whose asynq task is
// still pending, scheduled, retrying or archived is invisible here by design —
// this binary holds no Redis client — so the operational contract requires
// cmd/enhancement-drain to have proven the queue side first. The Hostinger
// wrapper runs it; see docs/launch/RUNBOOK.md.
//
// This command ends at a clean 41. It does not continue to 40, and starting the
// schema-41 application is a separate, deliberate act afterwards.
func rollbackSchema42(m *migrate.Migrate, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet(rollbackSchema42Command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	confirm := fs.String("confirm-production", "",
		"required acknowledgement when APP_ENV=production; must be exactly "+rollbackSchema42Confirmation)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s takes no positional arguments; it reverts exactly schema %d to %d",
			rollbackSchema42Command, db.ActiveProcessingKindSchemaVersion, db.EnhancementRecoveryFoundationSchemaVersion)
	}

	if err := requireProductionAcknowledgement(cfg, fmt.Sprintf("the supervised schema %d to %d rollback",
		db.ActiveProcessingKindSchemaVersion, db.EnhancementRecoveryFoundationSchemaVersion),
		rollbackSchema42Confirmation, *confirm); err != nil {
		return err
	}
	if err := requireCleanSchemaAt(m, rollbackSchema42Command,
		db.ActiveProcessingKindSchemaVersion, db.EnhancementRecoveryFoundationSchemaVersion); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		return fmt.Errorf("opening supervised rollback preflight connection: %w", err)
	}
	defer pool.Close()
	// The same helper the generic down path uses when it crosses 42 -> 41, so the
	// supervised command cannot drift into a weaker one.
	if err := db.CheckActiveProcessingKindRollbackSafety(ctx, pool); err != nil {
		return err
	}
	if err := db.CheckNoActiveMediaClaims(ctx, pool); err != nil {
		return err
	}
	if err := db.CheckNoPendingEnhancementIntents(ctx, pool); err != nil {
		return err
	}

	// Deliberately NOT CheckEnhancementRecoveryRollbackSafety: historical
	// ENHANCEMENT and FINALIZATION attempts stay fully representable on schema
	// 41 and must not be deleted to satisfy a rollback. They close 41 -> 40,
	// which this command never performs.
	return stepDownOnce(m, rollbackSchema42Command, db.EnhancementRecoveryFoundationSchemaVersion)
}

func version(m *migrate.Migrate) error {
	return report(m, "version")
}

// maxVersion prints the highest schema version this build supports, as a bare
// integer on stdout so a shell can capture it without parsing prose.
func maxVersion() error {
	_, err := fmt.Fprintf(os.Stdout, "%d\n", db.MaxSchemaVersion)
	return err
}

// requireClean refuses to act on a database left dirty by a failed migration.
// Stacking another migration onto an unknown half-applied state is how a
// recoverable failure becomes an unrecoverable one.
func requireClean(m *migrate.Migrate) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil // never migrated; nothing to be dirty
	}
	if err != nil {
		return fmt.Errorf("reading current schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("schema is dirty at version %d; resolve it manually before migrating", v)
	}
	return nil
}

// report prints the resulting version and whether this build supports it, so a
// release step surfaces an incompatibility immediately rather than leaving it
// for the first readiness probe.
func report(m *migrate.Migrate, action string) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Printf("migrate %s: schema is uninitialized (supported: %d..%d)\n",
			action, db.MinSchemaVersion, db.MaxSchemaVersion)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading resulting schema version: %w", err)
	}

	supported := "supported"
	if v < db.MinSchemaVersion || v > db.MaxSchemaVersion {
		supported = "NOT supported by this build"
	}
	fmt.Printf("migrate %s: version=%d dirty=%t (%s; this build supports %d..%d)\n",
		action, v, dirty, supported, db.MinSchemaVersion, db.MaxSchemaVersion)

	if dirty {
		return fmt.Errorf("schema is dirty at version %d", v)
	}
	return nil
}
