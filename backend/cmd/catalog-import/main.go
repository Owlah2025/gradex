// Command catalog-import applies a checked-in Academic Catalog manifest.
//
// Three modes, all operating on embedded manifests only:
//
//	catalog-import -mode=validate -manifest=kuwait-university-launch-v1
//	catalog-import -mode=dry-run  -manifest=kuwait-university-launch-v1
//	catalog-import -mode=apply    -manifest=kuwait-university-launch-v1
//
// There is deliberately no path or URL flag. The manifest is selected by
// identifier and read from the binary, so no operator or caller can point the
// importer at arbitrary data.
//
// The command depends on the academic catalog only. It knows nothing about
// Courses, the legacy taxonomy, entitlements, or the frontend.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/academic/importer"
	"github.com/Owlah2025/gradex/backend/internal/academic/manifest"
)

// ImportActorDescriptor names the deployment principal in the audit trail. The
// importer has no human operator, so it uses the SYSTEM actor convention rather
// than borrowing or inventing an Admin account.
const ImportActorDescriptor = "system:catalog-import"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "catalog-import: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		mode        = flag.String("mode", "validate", "validate | dry-run | apply")
		manifestID  = flag.String("manifest", "", "manifest identifier (see -list)")
		list        = flag.Bool("list", false, "list the manifest identifiers compiled into this binary")
		asJSON      = flag.Bool("json", false, "emit the plan as JSON")
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
		timeout     = flag.Duration("timeout", 2*time.Minute, "overall timeout")
	)
	flag.Parse()

	if *list {
		ids, err := manifest.Available()
		if err != nil {
			return err
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return nil
	}
	if strings.TrimSpace(*manifestID) == "" {
		return errors.New("a -manifest identifier is required; use -list to see what is available")
	}

	pkg, err := manifest.Load(*manifestID)
	if err != nil {
		return err
	}

	if *mode == "validate" {
		// Validation is a pure function of checked-in data: it never touches a
		// database, so it is safe to run anywhere, including CI without Postgres.
		fmt.Printf("manifest %s v%s is valid: institution=%s units=%d programs=%d curricula=%d subjects=%d mappings=%d sources=%d\n",
			pkg.Manifest.ID, pkg.Manifest.Version, pkg.Manifest.Institution.Slug,
			len(pkg.Manifest.Units), len(pkg.Manifest.Programs), len(pkg.Manifest.Curricula),
			len(pkg.Manifest.Subjects), len(pkg.Manifest.Mappings), len(pkg.Sources.Sources))
		return nil
	}

	var apply bool
	switch *mode {
	case "dry-run":
		apply = false
	case "apply":
		apply = true
	default:
		return fmt.Errorf("unknown mode %q; expected validate, dry-run, or apply", *mode)
	}

	if strings.TrimSpace(*databaseURL) == "" {
		return errors.New("a database URL is required for dry-run and apply")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}
	defer pool.Close()

	repository, err := academic.NewRepository(pool)
	if err != nil {
		return err
	}
	catalogImporter, err := importer.New(repository)
	if err != nil {
		return err
	}

	plan, err := catalogImporter.Run(ctx, pkg, importer.Options{
		Actor: academic.SystemActor(ImportActorDescriptor),
		Apply: apply,
	})
	if err != nil {
		return err
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	printPlan(plan)
	return nil
}

func printPlan(plan *importer.Plan) {
	verb := "dry run"
	if plan.Applied {
		verb = "applied"
	}
	fmt.Printf("%s %s v%s against %s\n", verb, plan.ManifestID, plan.ManifestVersion, plan.InstitutionSlug)
	for _, step := range plan.Steps {
		fmt.Printf("  %-8s %-20s %s %s\n", step.Action, step.Entity, step.Key, step.Detail)
	}
	fmt.Printf("create=%d update=%d noop=%d drift=%d\n",
		plan.Counts.Create, plan.Counts.Update, plan.Counts.Noop, plan.Counts.Drift)
	if plan.Counts.Drift > 0 {
		// Drift is reported, never acted on. Absence from a manifest is not an
		// instruction to remove real academic data.
		fmt.Println("note: drift rows are retained; this importer never retires or deletes by omission")
	}
}
