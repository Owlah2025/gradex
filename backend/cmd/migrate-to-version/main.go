//go:build integration

// migrate-to-version stages a disposable database at an EXACT schema version.
//
// It exists for verification scripts, not for operations. cmd/migrate is the only
// migration entry point for a real database: it loads and validates the full
// production configuration, refuses generic down migrations outside development,
// and gates the supervised rollbacks behind an explicit acknowledgement. None of
// that is wanted — or safe to bypass — for a throwaway database a script just
// created.
//
// The build tag keeps it out of every normal build, so it can never be shipped in
// an image or reached in production.
//
// Usage: migrate-to-version <source-url> <dsn> <version>
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: migrate-to-version <source-url> <dsn> <version>")
		os.Exit(2)
	}
	version, err := strconv.ParseUint(os.Args[3], 10, 32)
	if err != nil || version == 0 {
		fmt.Fprintf(os.Stderr, "invalid target version %q\n", os.Args[3])
		os.Exit(2)
	}
	m, err := migrate.New(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "opening migrator:", err)
		os.Exit(1)
	}
	defer m.Close()
	if err := m.Migrate(uint(version)); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintln(os.Stderr, "migrating:", err)
		os.Exit(1)
	}
	// The marker is verified rather than assumed: a dirty or unexpected version
	// would make every later assertion in the calling script meaningless.
	reached, dirty, err := m.Version()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading schema version:", err)
		os.Exit(1)
	}
	if uint64(reached) != version || dirty {
		fmt.Fprintf(os.Stderr, "staged schema = version=%d dirty=%t, want clean %d\n", reached, dirty, version)
		os.Exit(1)
	}
	fmt.Printf("staged schema %d clean\n", reached)
}
