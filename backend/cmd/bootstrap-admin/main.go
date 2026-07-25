// Command bootstrap-admin creates the one bootstrap Administrator.
//
// This is link 2 of the S1 bootstrap chain and implements the API/security
// design §4.5. It is a controlled one-off release command in its own
// entrypoint — deliberately not an HTTP endpoint, not an ordinary worker task,
// and not a schema migration:
//
//   - An HTTP endpoint would put Admin creation on the public attack surface
//     for the entire life of the platform to serve one operation.
//   - A worker task would make it reachable by anything that can enqueue.
//   - A migration would put the operation, and worse its password, into a file
//     in Git that runs on every environment.
//
// The initial password is read only through the configured secret resolver.
// There is no --password flag, and adding one would be a security regression:
// process arguments are visible to every other process on the host, land in
// shell history, and are captured by process supervisors. See
// TestNoPasswordFlagExists.
//
// Usage:
//
//	BOOTSTRAP_ADMIN_PASSWORD=... go run ./cmd/bootstrap-admin \
//	  -email admin@example.com \
//	  -display-name "Platform Administrator" \
//	  -operation-id 2026-07-29-initial \
//	  -principal "deploy@ci" \
//	  -confirm-production        # required only when APP_ENV=production
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// passwordSecretName is the reference the resolver looks up. Today that is an
// environment variable; once a secret manager is approved it becomes a path,
// and nothing here changes.
const passwordSecretName = "BOOTSTRAP_ADMIN_PASSWORD"

// commandTimeout bounds the whole operation. Argon2id hashing is intentionally
// slow and the advisory lock may be held by a concurrent attempt, so this is
// generous — but unbounded would let a stuck deploy hang a release forever.
const commandTimeout = 2 * time.Minute

func main() {
	scrub := newScrubber()
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap-admin: %s\n", scrub(err.Error()))
		os.Exit(1)
	}
}

// newScrubber removes credential-bearing configuration from anything on its way
// to output, the same defense cmd/migrate uses. The bootstrap password is the
// obvious one, but the DSN carries the database password and a failure here is
// exactly when output gets pasted into a ticket.
//
// This is defense in depth, not the primary control: the password is held in a
// config.Secret that redacts itself under every formatting verb, and no code
// path formats it. This catches the case where a driver or library error
// happens to echo a value back.
func newScrubber() func(string) string {
	var secrets []string
	for _, key := range []string{passwordSecretName, "DATABASE_URL"} {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		secrets = append(secrets, v)
		if u, err := url.Parse(v); err == nil && u.User != nil {
			if pw, set := u.User.Password(); set && pw != "" {
				secrets = append(secrets, pw)
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

type options struct {
	email             string
	displayName       string
	operationID       string
	principal         string
	confirmProduction bool
}

// registerFlags declares the command's entire argument surface. It is separate
// from parseFlags so a test can inspect the real flag set and fail if a
// credential-bearing flag is ever added — see TestNoFlagAcceptsAPassword.
func registerFlags(fs *flag.FlagSet) *options {
	var opts options
	fs.StringVar(&opts.email, "email", "", "Administrator email address (normalized to lowercase)")
	fs.StringVar(&opts.displayName, "display-name", "", "Administrator display name")
	fs.StringVar(&opts.operationID, "operation-id", "", "stable ID for this operation; rerunning with the same ID is a retry")
	fs.StringVar(&opts.principal, "principal", "", "deployment principal running this operation, recorded in Audit")
	fs.BoolVar(&opts.confirmProduction, "confirm-production", false, "required acknowledgement when APP_ENV=production")
	return &opts
}

func parseFlags(args []string, out *os.File) (options, error) {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	if out != nil {
		fs.SetOutput(out)
	}
	opts := registerFlags(fs)

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected positional argument %q", fs.Arg(0))
	}
	return *opts, nil
}

func run(args []string, stdout *os.File) error {
	opts, err := parseFlags(args, stdout)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// The explicit production configuration gate required by §4.5. Creating the
	// platform's only Administrator against production is not something anyone
	// should be able to do by having the wrong APP_ENV exported in a shell.
	if cfg.Environment().IsProduction() && !opts.confirmProduction {
		return errors.New("APP_ENV=production requires -confirm-production")
	}
	// The inverse is also worth refusing: passing the production flag against a
	// non-production database usually means the operator believes they are
	// somewhere they are not.
	if !cfg.Environment().IsProduction() && opts.confirmProduction {
		return fmt.Errorf("-confirm-production was passed but APP_ENV=%s", cfg.Environment())
	}

	// The password comes from the secret boundary and nowhere else.
	resolver := config.EnvSecretResolver{}
	password, err := resolver.Resolve(config.SecretRef{Name: passwordSecretName, Required: true})
	if err != nil {
		return fmt.Errorf("resolving %s: %w", passwordSecretName, err)
	}
	if password.IsEmpty() {
		return fmt.Errorf("%s is not set; the initial password must be supplied through the secret boundary, never a flag", passwordSecretName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// A single connection rather than a pool: this is one transaction that must
	// hold an advisory lock, and a pool would add the possibility of the
	// transaction and the lock landing on different connections.
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	result, err := identity.Bootstrap(ctx, conn, identity.BootstrapRequest{
		OperationID:         opts.operationID,
		Email:               opts.email,
		DisplayName:         opts.displayName,
		Password:            password,
		DeploymentPrincipal: opts.principal,
	})
	if err != nil {
		return err
	}

	// Output carries no credential material — only what an operator needs to
	// confirm the operation and find it in Audit.
	if result.AlreadyCompleted {
		fmt.Fprintf(stdout, "bootstrap-admin: already completed for operation %q at %s (account %s)\n",
			result.OperationID, result.CompletedAt.UTC().Format(time.RFC3339), result.AccountID)
		fmt.Fprintln(stdout, "bootstrap-admin: nothing to do; no second Administrator was created")
		return nil
	}

	fmt.Fprintf(stdout, "bootstrap-admin: created Administrator %s (account %s) at %s\n",
		result.Email, result.AccountID, result.CompletedAt.UTC().Format(time.RFC3339))
	fmt.Fprintln(stdout, "bootstrap-admin: the credential is CHANGE_REQUIRED; the first sign-in must change it")
	return nil
}
