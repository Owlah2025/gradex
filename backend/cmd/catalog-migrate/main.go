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
	"strings"
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

	// Every step is printed. Nothing is filtered out of the report, because a
	// row this tool declines to show is a row the operator cannot account for.
	for _, step := range plan.Steps {
		mutate := " "
		if step.WouldMutate {
			mutate = "*"
		}
		fmt.Printf("%s %-24s %s  %s\n", mutate, step.Outcome, step.CourseID, step.TitleEn)
		if step.LegacyCode != "" || step.LegacyLabel != "" {
			fmt.Printf("      legacy:   %s %s\n", displayOrDash(step.LegacyCode), displayOrDash(step.LegacyLabel))
		}
		if step.CurrentSubject != "" {
			fmt.Printf("      current:  %s\n", step.CurrentSubject)
		}
		if step.SubjectCode != "" {
			fmt.Printf("      target:   %s (%s)\n", step.SubjectCode, displayOrDash(step.MappingSource))
		}
		if len(step.Candidates) > 0 {
			fmt.Printf("      candidates: %s\n", strings.Join(step.Candidates, ", "))
		}
		if step.Disposition != "" {
			fmt.Printf("      founder:  %s%s\n", step.Disposition, decidedSuffix(step.DecidedOn))
		}
		if len(step.ResolutionRequires) > 0 {
			fmt.Printf("      reopens on: %s\n", strings.Join(step.ResolutionRequires, "; "))
		}
		if step.ProgramWord != "" {
			fmt.Printf("      audience: %s\n", step.ProgramWord)
		}
		fmt.Printf("      reason:   %s\n", step.Detail)
	}

	counts := plan.Counts
	fmt.Printf("\ntotal=%d migrate=%d already_academic=%d unmapped=%d ambiguous=%d"+
		" founder_mapping_required=%d no_revision=%d ineligible=%d drift=%d\n",
		counts.Total, counts.Migrate, counts.AlreadyAcademic, counts.Unmapped,
		counts.Ambiguous, counts.FounderMappingRequired, counts.NoRevision,
		counts.Ineligible, counts.Drift)

	// The reconciliation line. The counts must add up to the rows, and the tool
	// says so itself rather than leaving the reader to add them.
	if len(plan.Steps) != counts.Total {
		return fmt.Errorf("report is inconsistent: %d rows but total=%d", len(plan.Steps), counts.Total)
	}
	if !plan.Applied && counts.Migrate > 0 {
		fmt.Printf("\nRows marked * would be written. Re-run with --apply to migrate the %d Course(s).\n",
			counts.Migrate)
	}
	// Only genuinely open questions are announced as work. A record the Founder
	// has decided to keep unresolved is a finished state, and reporting it as
	// outstanding would misdescribe the corpus every single run.
	if open := openFounderDecisions(plan); open > 0 {
		fmt.Printf("\n%d Course(s) need a Founder mapping decision before they can migrate.\n", open)
	}
	if accepted := counts.FounderMappingRequired - openFounderDecisions(plan); accepted > 0 {
		fmt.Printf("\n%d Course(s) are intentionally unresolved by Founder decision and are not pending work.\n",
			accepted)
	}
	return nil
}

// openFounderDecisions counts only the pending terms nobody has answered yet.
func openFounderDecisions(plan *legacymigrate.Plan) int {
	open := 0
	for _, step := range plan.Steps {
		if step.Outcome != legacymigrate.OutcomeFounderMappingRequired {
			continue
		}
		if step.Disposition == "" || step.Disposition == string(legacymigrate.DispositionAwaitingDecision) {
			open++
		}
	}
	return open
}

func decidedSuffix(decidedOn string) string {
	if strings.TrimSpace(decidedOn) == "" {
		return ""
	}
	return " (" + decidedOn + ")"
}

func displayOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
