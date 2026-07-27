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

func usage() error {
	return errors.New("usage: migrate <up|down|version|max-version> [steps]")
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
	case "down":
		return down(m, cfg, args[1:])
	case "version":
		return version(m)
	default:
		return usage()
	}
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

	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("reverting migrations: %w", err)
	}
	return report(m, "down")
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
