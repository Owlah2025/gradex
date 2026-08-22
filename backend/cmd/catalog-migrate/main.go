// Command catalog-migrate carries Courses from the legacy taxonomy
// classification onto the canonical Academic Catalog (D-091 §13, T5).
//
//	catalog-migrate --list
//	catalog-migrate --mapping <id> --report
//	catalog-migrate --mapping <id> --apply
//
// --report is the default and never writes: the plan is computed inside a
// transaction that is always rolled back. --apply must be asked for explicitly,
// and migrates only the Courses the report classified MIGRATE. Every other
// outcome leaves its Course on the legacy classification, fully operational.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/legacymigrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "catalog-migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		mappingID = flag.String("mapping", "", "embedded legacy mapping identifier")
		apply     = flag.Bool("apply", false, "execute the migration; without this the run only reports")
		list      = flag.Bool("list", false, "list the embedded mapping identifiers")
		actor     = flag.String("actor", "catalog-migrate", "descriptor audited on every migrated Course")
		asJSON    = flag.Bool("json", false, "emit the plan as JSON")
	)
	flag.Parse()

	if *list {
		available, err := legacymigrate.Available()
		if err != nil {
			return err
		}
		for _, id := range available {
			fmt.Println(id)
		}
		return nil
	}
	if *mappingID == "" {
		return fmt.Errorf("--mapping is required; use --list to see the embedded identifiers")
	}

	// The database URL is read from the environment rather than a flag, so an
	// operator cannot point a migration at an arbitrary database by typo.
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	mapping, err := legacymigrate.Load(*mappingID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	migrator, err := legacymigrate.New(pool)
	if err != nil {
		return err
	}
	plan, err := migrator.Run(ctx, mapping, legacymigrate.Options{
		ActorDescriptor: *actor, Apply: *apply,
	})
	if err != nil {
		return err
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	return writeReport(plan)
}

func writeReport(plan *legacymigrate.Plan) error {
	mode := "REPORT (nothing was written)"
	if plan.Applied {
		mode = "APPLIED"
	}
	fmt.Printf("mapping:     %s %s\n", plan.MappingID, plan.MappingVersion)
	fmt.Printf("institution: %s\n", plan.InstitutionSlug)
	fmt.Printf("mode:        %s\n\n", mode)
	for _, step := range plan.Steps {
		if step.CourseID == "" {
			continue
		}
		fmt.Printf("  %-16s %s  %s\n", step.Outcome, step.CourseID, step.Detail)
	}
	fmt.Printf("\nmigrate=%d unmapped=%d ambiguous=%d ineligible=%d already-academic=%d\n",
		plan.Counts.Migrate, plan.Counts.Unmapped, plan.Counts.Ambiguous,
		plan.Counts.Ineligible, plan.Counts.AlreadyAcademic)
	if !plan.Applied && plan.Counts.Migrate > 0 {
		fmt.Printf("\nRe-run with --apply to migrate the %d Course(s) above.\n", plan.Counts.Migrate)
	}
	return nil
}
