//go:build integration

package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// Its own database, like the migration test, so a run can recreate the whole
// schema without touching a developer's working data.
const (
	adminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	testDBName = "gradex_identity_test"
	testDSN    = "postgres://gradex:gradex@localhost:5432/" + testDBName + "?sslmode=disable"
	sourceURL  = "file://../db/migrations"
	opTimeout  = 60 * time.Second
)

// freshSchema drops and recreates the test database, then migrates it up, so
// every test starts from a known-empty Identity.
func freshSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connecting to the admin database: %v", err)
	}
	defer admin.Close()

	// Terminate stragglers first; DROP DATABASE fails while a connection
	// remains open, and a previous failed test can leave one behind.
	if _, err := admin.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, testDBName,
	); err != nil {
		t.Fatalf("terminating existing connections: %v", err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+testDBName); err != nil {
		t.Fatalf("dropping the test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+testDBName); err != nil {
		t.Fatalf("creating the test database: %v", err)
	}

	m, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating up: %v", err)
	}
}

func connect(t *testing.T) (*pgx.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)

	conn, err := pgx.Connect(ctx, testDSN)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn, ctx
}

func request(operationID string) BootstrapRequest {
	return BootstrapRequest{
		OperationID:         operationID,
		Email:               "Admin@Gradex.Example",
		DisplayName:         "Platform Administrator",
		Password:            config.NewSecret("correct-horse-battery-staple-7"),
		DeploymentPrincipal: "deploy@ci",
		Compromised:         clearCompromisedSource(),
	}
}

func clearCompromisedSource() CompromisedRangeSource {
	source, err := NewDeterministicCompromisedSource()
	if err != nil {
		panic("constructing deterministic clear source: " + err.Error())
	}
	return source
}

func TestBootstrapCreatesTheAdministrator(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	result, err := Bootstrap(ctx, conn, request("op-1"))
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}
	if result.AlreadyCompleted {
		t.Fatal("the first bootstrap reported itself as a retry")
	}

	// The correspondence form is preserved while the separate normalized value
	// remains the uniqueness key (S1B1 admission prerequisite).
	if result.Email != "Admin@Gradex.Example" {
		t.Fatalf("correspondence email was not preserved: %q", result.Email)
	}

	var (
		role, status, credentialState string
		normalizedEmail, email        string
		verifiedAt                    *time.Time
	)
	if err := conn.QueryRow(ctx,
		`SELECT a.role, a.status, c.state, a.normalized_email, a.email, a.email_verified_at
		   FROM accounts a JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.id = $1`, result.AccountID,
	).Scan(&role, &status, &credentialState, &normalizedEmail, &email, &verifiedAt); err != nil {
		t.Fatalf("reading the created Account: %v", err)
	}
	if normalizedEmail != "admin@gradex.example" || email != "Admin@Gradex.Example" {
		t.Fatalf("email forms = normalized %q/correspondence %q", normalizedEmail, email)
	}

	if role != "ADMIN" {
		t.Fatalf("role = %q, want ADMIN", role)
	}
	// The two independent facts link 1's schema was shaped to express: the
	// Account is usable, and the credential is not yet.
	if status != "ACTIVE" {
		t.Fatalf("status = %q, want ACTIVE", status)
	}
	if credentialState != "CHANGE_REQUIRED" {
		t.Fatalf("credential state = %q, want CHANGE_REQUIRED", credentialState)
	}
	if verifiedAt == nil {
		t.Fatal("the bootstrap Admin should be created already verified")
	}

	// Audit evidence committed in the same transaction (domain design §17).
	var auditCount int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM audit_events
		  WHERE action = 'BOOTSTRAP_ADMIN_CREATED' AND target_id = $1 AND operation_id = $2`,
		result.AccountID, "op-1",
	).Scan(&auditCount); err != nil {
		t.Fatalf("reading audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", auditCount)
	}
}

// Bootstrap close condition 1: bootstrap cannot complete twice — same
// operation ID, different operation ID, or concurrent.
func TestBootstrapCannotCompleteTwice(t *testing.T) {
	t.Run("same operation ID returns the recorded result", func(t *testing.T) {
		freshSchema(t)
		conn, ctx := connect(t)

		first, err := Bootstrap(ctx, conn, request("op-same"))
		if err != nil {
			t.Fatalf("first bootstrap: %v", err)
		}

		second, err := Bootstrap(ctx, conn, request("op-same"))
		if err != nil {
			t.Fatalf("retry with the same operation ID should succeed: %v", err)
		}
		if !second.AlreadyCompleted {
			t.Fatal("retry did not report itself as already completed")
		}
		if second.AccountID != first.AccountID {
			t.Fatalf("retry returned a different Account: %q vs %q", second.AccountID, first.AccountID)
		}
		assertAdminCount(t, ctx, conn, 1)
	})

	t.Run("same operation ID with changed semantics fails closed", func(t *testing.T) {
		tests := map[string]func(*BootstrapRequest){
			"correspondence email": func(r *BootstrapRequest) { r.Email = "admin@gradex.example" },
			"display name":         func(r *BootstrapRequest) { r.DisplayName = "Another Administrator" },
			"principal":            func(r *BootstrapRequest) { r.DeploymentPrincipal = "other-deploy@ci" },
			"password":             func(r *BootstrapRequest) { r.Password = config.NewSecret("a-different-secure-passphrase-8") },
		}

		for name, mutate := range tests {
			t.Run(name, func(t *testing.T) {
				freshSchema(t)
				conn, ctx := connect(t)

				if _, err := Bootstrap(ctx, conn, request("op-fingerprint")); err != nil {
					t.Fatalf("first bootstrap: %v", err)
				}
				retry := request("op-fingerprint")
				mutate(&retry)
				if _, err := Bootstrap(ctx, conn, retry); !errors.Is(err, ErrBootstrapRequestMismatch) {
					t.Fatalf("retry error = %v, want ErrBootstrapRequestMismatch", err)
				}
				assertAdminCount(t, ctx, conn, 1)
			})
		}
	})

	t.Run("legacy marker without fingerprint fails closed", func(t *testing.T) {
		freshSchema(t)
		conn, ctx := connect(t)

		if _, err := Bootstrap(ctx, conn, request("op-legacy")); err != nil {
			t.Fatalf("first bootstrap: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`UPDATE bootstrap_operations
			    SET fingerprint_version = NULL, request_fingerprint = NULL
			  WHERE singleton`,
		); err != nil {
			t.Fatalf("simulating legacy marker: %v", err)
		}
		if _, err := Bootstrap(ctx, conn, request("op-legacy")); !errors.Is(err, ErrBootstrapRetryUnverifiable) {
			t.Fatalf("retry error = %v, want ErrBootstrapRetryUnverifiable", err)
		}
		assertAdminCount(t, ctx, conn, 1)
	})

	t.Run("different operation ID fails closed", func(t *testing.T) {
		freshSchema(t)
		conn, ctx := connect(t)

		if _, err := Bootstrap(ctx, conn, request("op-first")); err != nil {
			t.Fatalf("first bootstrap: %v", err)
		}

		_, err := Bootstrap(ctx, conn, request("op-second"))
		if !errors.Is(err, ErrBootstrapAlreadyCompleted) {
			t.Fatalf("expected ErrBootstrapAlreadyCompleted, got %v", err)
		}
		assertAdminCount(t, ctx, conn, 1)
	})

	t.Run("concurrent attempts mint exactly one Admin", func(t *testing.T) {
		freshSchema(t)

		const attempts = 8
		var (
			wg        sync.WaitGroup
			mu        sync.Mutex
			created   int
			retried   int
			refused   int
			otherErrs []error
		)

		for i := 0; i < attempts; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
				defer cancel()

				conn, err := pgx.Connect(ctx, testDSN)
				if err != nil {
					mu.Lock()
					otherErrs = append(otherErrs, err)
					mu.Unlock()
					return
				}
				defer func() { _ = conn.Close(context.Background()) }()

				// Half race on the same operation ID (a retry storm from a
				// deploy runner) and half on distinct IDs (two operators).
				id := "op-concurrent"
				if i%2 == 1 {
					id = fmt.Sprintf("op-concurrent-%d", i)
				}

				result, err := Bootstrap(ctx, conn, request(id))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil && result.AlreadyCompleted:
					retried++
				case err == nil:
					created++
				case errors.Is(err, ErrBootstrapAlreadyCompleted):
					refused++
				default:
					otherErrs = append(otherErrs, err)
				}
			}(i)
		}
		wg.Wait()

		if len(otherErrs) > 0 {
			t.Fatalf("unexpected errors from concurrent attempts: %v", otherErrs)
		}
		if created != 1 {
			t.Fatalf("expected exactly 1 creating attempt, got %d (retries %d, refusals %d)",
				created, retried, refused)
		}

		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		conn, err := pgx.Connect(ctx, testDSN)
		if err != nil {
			t.Fatalf("connecting for verification: %v", err)
		}
		defer func() { _ = conn.Close(context.Background()) }()
		assertAdminCount(t, ctx, conn, 1)
	})
}

// Bootstrap close condition 2, database half: only the Argon2id hash reaches
// the database. The plaintext must appear in no column of any table.
func TestBootstrapPersistsOnlyTheArgon2idHash(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	const plaintext = "a-very-distinctive-plaintext-value-42"
	req := request("op-secrecy")
	req.Password = config.NewSecret(plaintext)

	result, err := Bootstrap(ctx, conn, req)
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}

	var stored string
	if err := conn.QueryRow(ctx,
		`SELECT password_hash FROM password_credentials WHERE account_id = $1`, result.AccountID,
	).Scan(&stored); err != nil {
		t.Fatalf("reading the stored credential: %v", err)
	}
	if !strings.HasPrefix(stored, "$argon2id$") {
		t.Fatalf("stored credential is not an Argon2id hash: %q", stored)
	}
	if strings.Contains(stored, plaintext) {
		t.Fatal("the stored credential contains the plaintext")
	}

	ok, err := VerifyPassword(stored, plaintext)
	if err != nil {
		t.Fatalf("verifying the stored hash: %v", err)
	}
	if !ok {
		t.Fatal("the stored hash does not verify against the password it was made from")
	}

	// Sweep every text-ish column in the database rather than only the columns
	// this test knows about. A future migration that adds a column someone
	// writes the password into should fail here.
	assertPlaintextAbsentEverywhere(t, ctx, conn, plaintext)
}

// Bootstrap close condition 5: failure midway leaves neither a second usable
// Admin nor a falsely completed bootstrap marker.
func TestBootstrapFailureRollsBackWhole(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	// Force the final INSERT to fail by removing the column it writes. The
	// Account, credential, and Audit row are all written before this point, so
	// if the transaction were not whole, they would survive.
	if _, err := conn.Exec(ctx,
		`ALTER TABLE bootstrap_operations DROP COLUMN operation_id`,
	); err != nil {
		t.Fatalf("preparing the failure: %v", err)
	}

	_, err := Bootstrap(ctx, conn, request("op-doomed"))
	if err == nil {
		t.Fatal("expected the bootstrap to fail")
	}

	assertAdminCount(t, ctx, conn, 0)

	var credentials, audits, markers int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM password_credentials`).Scan(&credentials); err != nil {
		t.Fatalf("counting credentials: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&audits); err != nil {
		t.Fatalf("counting audit events: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM bootstrap_operations`).Scan(&markers); err != nil {
		t.Fatalf("counting bootstrap markers: %v", err)
	}

	if credentials != 0 {
		t.Fatalf("a credential survived a failed bootstrap: %d rows", credentials)
	}
	if audits != 0 {
		t.Fatalf("an audit event survived a failed bootstrap: %d rows", audits)
	}
	if markers != 0 {
		t.Fatalf("a bootstrap marker survived a failed bootstrap: %d rows", markers)
	}
}

// An Admin that exists without a recorded bootstrap operation — a restore, or
// manual intervention — must stop the operation rather than mint a second one.
func TestBootstrapRefusesWhenAnAdminAlreadyExists(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	if _, err := conn.Exec(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('prior@gradex.example', 'prior@gradex.example', 'ADMIN', 'ACTIVE', 'Prior Admin')`,
	); err != nil {
		t.Fatalf("inserting the pre-existing Admin: %v", err)
	}

	_, err := Bootstrap(ctx, conn, request("op-after-restore"))
	if !errors.Is(err, ErrAdminAlreadyExists) {
		t.Fatalf("expected ErrAdminAlreadyExists, got %v", err)
	}
	assertAdminCount(t, ctx, conn, 1)
}

func TestBootstrapRejectsPolicyViolatingPassword(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	req := request("op-weak")
	req.Password = config.NewSecret("short")

	_, err := Bootstrap(ctx, conn, req)
	if !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("expected ErrPasswordPolicy, got %v", err)
	}
	assertAdminCount(t, ctx, conn, 0)
}

func TestBootstrapRejectsMalformedInput(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	tests := map[string]func(*BootstrapRequest){
		"empty operation ID":       func(r *BootstrapRequest) { r.OperationID = "  " },
		"empty email":              func(r *BootstrapRequest) { r.Email = "" },
		"email without at":         func(r *BootstrapRequest) { r.Email = "not-an-address" },
		"email without dot":        func(r *BootstrapRequest) { r.Email = "admin@localhost" },
		"email with inner space":   func(r *BootstrapRequest) { r.Email = "ad min@gradex.example" },
		"email with inner newline": func(r *BootstrapRequest) { r.Email = "admin\n@gradex.example" },
		"empty display name":       func(r *BootstrapRequest) { r.DisplayName = " " },
		"missing principal":        func(r *BootstrapRequest) { r.DeploymentPrincipal = "" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			req := request("op-malformed")
			mutate(&req)
			if _, err := Bootstrap(ctx, conn, req); err == nil {
				t.Fatal("expected the malformed request to be rejected")
			}
			assertAdminCount(t, ctx, conn, 0)
		})
	}
}

// Audit is append-only (domain design §17). Evidence that can be edited after
// the fact is not evidence.
func TestAuditEventsAreAppendOnly(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	if _, err := Bootstrap(ctx, conn, request("op-audit")); err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}

	if _, err := conn.Exec(ctx, `UPDATE audit_events SET reason = 'rewritten'`); err == nil {
		t.Fatal("an audit event was updated; the table is not append-only")
	}
	if _, err := conn.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Fatal("an audit event was deleted; the table is not append-only")
	}
}

func assertAdminCount(t *testing.T, ctx context.Context, conn *pgx.Conn, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE role = 'ADMIN'`).Scan(&got); err != nil {
		t.Fatalf("counting Admin Accounts: %v", err)
	}
	if got != want {
		t.Fatalf("Admin Account count = %d, want %d", got, want)
	}
}

// assertPlaintextAbsentEverywhere scans every character-typed and JSON column
// in the public schema. This is broader than the columns the bootstrap writes
// on purpose: the guarantee is about the database, not about the statements
// this version of the code happens to run.
func assertPlaintextAbsentEverywhere(t *testing.T, ctx context.Context, conn *pgx.Conn, plaintext string) {
	t.Helper()

	rows, err := conn.Query(ctx, `
		SELECT table_name, column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND data_type IN ('text', 'character varying', 'character', 'json', 'jsonb')`)
	if err != nil {
		t.Fatalf("listing columns: %v", err)
	}

	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name); err != nil {
			rows.Close()
			t.Fatalf("scanning column list: %v", err)
		}
		columns = append(columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("reading column list: %v", err)
	}
	if len(columns) == 0 {
		t.Fatal("found no text columns to scan; the sweep would pass vacuously")
	}

	for _, c := range columns {
		query := fmt.Sprintf(
			`SELECT count(*) FROM %q WHERE %q::text LIKE '%%' || $1 || '%%'`, c.table, c.name)
		var hits int
		if err := conn.QueryRow(ctx, query, plaintext).Scan(&hits); err != nil {
			t.Fatalf("scanning %s.%s: %v", c.table, c.name, err)
		}
		if hits > 0 {
			t.Fatalf("the plaintext password appears in %s.%s", c.table, c.name)
		}
	}
}
