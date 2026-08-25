package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// T069: S5 owns report creation; AD-14 owns the separate Admin resolution surface (FR-035, BR-146).
//
// A report is a signal, not an automatic decision. Student creation remains under /learn, while
// Admin resolution is mounted separately behind ADMIN_OPERATIONS and the existing lifecycle command.
//
// Proving that needs care, because the router genuinely does mount routes that delist, retire, and
// suspend — `POST /api/v1/admin/courses/:id/delist`, `/retire`, `/access-suspension`, the taxonomy
// retirement route, and the Account suspension group. Those are S2's Course lifecycle and S1's
// Account administration, and they predate S5. Banning them would be wrong. What T069 asserts is
// narrower and is the thing that would actually be a breach: that S5 added no route which acts on a
// report, and that no route anywhere accepts a report identifier or reaches report state.

// s5RouteGroup is the surface S5 owns. Everything mounted under it is S5's to justify.
const s5RouteGroup = "/api/v1/learn/"

// acceptedS5Routes is the complete S5 route set as T063–T066 left it. The list is exhaustive on
// purpose: a new S5 route — moderation-shaped or not — fails here until it is justified, which is
// stronger than pattern-matching for words like "resolve".
var acceptedS5Routes = map[string]string{
	"GET /api/v1/learn/dashboard":                           "Dashboard read (T023)",
	"GET /api/v1/learn/courses/:courseId":                   "Course Home read (T024)",
	"GET /api/v1/learn/courses/:courseId/lessons/:lessonId": "Lesson read (T025)",
	"POST /api/v1/learn/lessons/:lessonId/playback":         "playback issuance (T026)",
	"PUT /api/v1/learn/lessons/:lessonId/progress":          "Progress write (T031)",
	"POST /api/v1/learn/reports":                            "report submission (T063)",
}

// theOnlyReportRoute is the single route in the entire API permitted to mention a report, and it
// only creates one.
const theOnlyReportRoute = "POST /api/v1/learn/reports"

// moderationVerbs are the actions FR-035 reserves for S8. They are matched against a route's path,
// and only ever combined with a report reference — the words alone are legitimate elsewhere.
// Stems, not whole words: "suspen" catches both `suspend` and `access-suspension`, and "resolv"
// catches `resolve` and `resolution`. A route that escapes classification is the failure mode this
// list exists to prevent.
var moderationVerbs = []string{
	"resolv", "dismiss", "close", "reopen", "moderat", "review",
	"delist", "retire", "quarantine", "takedown", "suspen", "assign", "queue", "triage",
}

// reportIdentifierPattern matches a path segment that would carry one report to an operation.
var reportIdentifierPattern = regexp.MustCompile(`(?i)/reports?/|:report`)

func mountedRoutes(t *testing.T) []string {
	t.Helper()
	router, _ := authzRouter(t, fixedPrincipals{})
	routes := make([]string, 0)
	for _, route := range router.Routes() {
		routes = append(routes, route.Method+" "+route.Path)
	}
	sort.Strings(routes)
	if len(routes) == 0 {
		t.Fatal("the production router mounted no routes at all")
	}
	return routes
}

// TestOnlyOneMountedRouteMentionsAReportAndItOnlyCreatesOne is the inventory at its narrowest: the
// whole production route table is searched for anything report-shaped.
func TestMountedReportRoutesSeparateStudentCreationFromAdminResolution(t *testing.T) {
	reportRoutes := make([]string, 0)
	for _, route := range mountedRoutes(t) {
		if strings.Contains(strings.ToLower(route), "report") {
			reportRoutes = append(reportRoutes, route)
		}
	}

	want := []string{
		"GET /api/v1/admin/reports",
		"GET /api/v1/admin/reports/:id",
		"POST /api/v1/admin/reports/:id/resolve",
		theOnlyReportRoute,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(reportRoutes, want) {
		t.Fatalf("routes mentioning a report = %v, want %v", reportRoutes, want)
	}

	// The only report identifier routes are the Admin detail and terminal resolution routes.
	for _, route := range mountedRoutes(t) {
		if reportIdentifierPattern.MatchString(route) && route != "GET /api/v1/admin/reports/:id" && route != "POST /api/v1/admin/reports/:id/resolve" {
			t.Fatalf("route %s accepts an unapproved report identifier", route)
		}
	}

	// And no route combines a report with a moderation verb.
	for _, route := range mountedRoutes(t) {
		lowered := strings.ToLower(route)
		if !strings.Contains(lowered, "report") {
			continue
		}
		for _, verb := range moderationVerbs {
			if strings.Contains(lowered, verb) {
				if route == "POST /api/v1/admin/reports/:id/resolve" {
					continue
				}
				t.Fatalf("route %s is an unapproved report %s route", route, verb)
			}
		}
	}
}

// TestS5MountsExactlyItsAcceptedRoutesAndNoModerationRoute closes the gap a name-based search
// leaves: a moderation route that never says "report" — `POST /learn/courses/:id/retire`, say —
// would still be S5 acting on reported content. The S5 surface is therefore pinned exactly.
func TestS5MountsExactlyItsAcceptedRoutesAndNoModerationRoute(t *testing.T) {
	mounted := make([]string, 0)
	for _, route := range mountedRoutes(t) {
		if strings.Contains(route, s5RouteGroup) {
			mounted = append(mounted, route)
		}
	}

	for _, route := range mounted {
		if _, accepted := acceptedS5Routes[route]; !accepted {
			t.Fatalf("S5 mounted an unaccepted route %s; if it is legitimate, justify it here, and if it "+
				"moderates a report it violates FR-035", route)
		}
		lowered := strings.ToLower(route)
		for _, verb := range moderationVerbs {
			if strings.Contains(lowered, verb) {
				t.Fatalf("S5 route %s carries the moderation verb %q", route, verb)
			}
		}
	}

	// A stale allowlist is itself a failure: every accepted route must still be mounted.
	mountedSet := make(map[string]bool, len(mounted))
	for _, route := range mounted {
		mountedSet[route] = true
	}
	for route, purpose := range acceptedS5Routes {
		if !mountedSet[route] {
			t.Fatalf("accepted S5 route %s (%s) is no longer mounted; the allowlist is stale", route, purpose)
		}
	}
	if len(mounted) != len(acceptedS5Routes) {
		t.Fatalf("S5 mounted %d routes, want the accepted %d: %v", len(mounted), len(acceptedS5Routes), mounted)
	}
}

// TestLifecycleAndSuspensionRoutesAreNotS5AndNotReportDriven is the classification T069 turns on.
//
// The API does delist, retire, and suspend — through S2's Course lifecycle and S1's Account
// administration, both of which predate S5 and are reached by an Admin acting on a Course or an
// Account, never on a report. This asserts S5 added none of them, rather than pretending they do
// not exist.
func TestLifecycleAndSuspensionRoutesAreNotS5AndNotReportDriven(t *testing.T) {
	lifecycleRoutes := make([]string, 0)
	for _, route := range mountedRoutes(t) {
		lowered := strings.ToLower(route)
		for _, verb := range []string{"delist", "retire", "suspen", "quarantine", "takedown"} {
			if strings.Contains(lowered, verb) {
				lifecycleRoutes = append(lifecycleRoutes, route)
				break
			}
		}
	}
	if len(lifecycleRoutes) == 0 {
		t.Fatal("no lifecycle routes found; this test can no longer prove S5 added none")
	}

	for _, route := range lifecycleRoutes {
		// None of them belongs to S5.
		if strings.Contains(route, s5RouteGroup) {
			t.Fatalf("S5 mounted the lifecycle route %s; delisting, retirement, and suspension are not S5's", route)
		}
		// None of them is reached with a report.
		if reportIdentifierPattern.MatchString(route) || strings.Contains(strings.ToLower(route), "report") {
			t.Fatalf("lifecycle route %s is reachable with a report identifier; a report must not trigger moderation", route)
		}
	}
	t.Logf("pre-existing non-S5 lifecycle routes, none report-driven: %v", lifecycleRoutes)
}

// productionSources returns every non-test Go file under backend/internal and backend/cmd.
func productionSources(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving backend root: %v", err)
	}
	sources := make(map[string]string)
	for _, directory := range []string{"internal", "cmd"} {
		walkRoot := filepath.Join(root, directory)
		if err := filepath.Walk(walkRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			sources[relative] = string(content)
			return nil
		}); err != nil {
			t.Fatalf("walking %s: %v", walkRoot, err)
		}
	}
	if len(sources) == 0 {
		t.Fatal("no production sources found; the audit would pass vacuously")
	}
	return sources
}

// TestReportResolutionCodeIsBoundedToTheAdminRepository is the source half of FR-035: only the
// report repository may update terminal resolution fields, and no production code deletes a report.
func TestReportResolutionCodeIsBoundedToTheAdminRepository(t *testing.T) {
	sources := productionSources(t)

	// Report creation and the Admin repository are the only production files that speak to the table.
	touching := make([]string, 0)
	for file, source := range sources {
		if strings.Contains(source, "content_reports") {
			touching = append(touching, file)
		}
	}
	sort.Strings(touching)
	wantTouching := []string{filepath.Join("internal", "learning", "report.go"), filepath.Join("internal", "learning", "report_admin.go")}
	sort.Strings(wantTouching)
	if !reflect.DeepEqual(touching, wantTouching) {
		t.Fatalf("production files touching content_reports = %v, want %v", touching, wantTouching)
	}

	// No production statement deletes a report or clears its terminal state.
	for file, source := range sources {
		lowered := strings.ToLower(source)
		for _, forbidden := range []string{
			"update content_reports",
			"delete from content_reports",
		} {
			if file == filepath.Join("internal", "learning", "report_admin.go") && forbidden == "update content_reports" {
				continue
			}
			if strings.Contains(lowered, forbidden) {
				t.Fatalf("%s contains %q; report history must remain immutable", file, forbidden)
			}
		}
	}

	// The Student writer still only inserts.
	reportSource := sources[filepath.Join("internal", "learning", "report.go")]
	if !strings.Contains(reportSource, "INSERT INTO content_reports") {
		t.Fatal("report.go no longer inserts; this audit is stale")
	}
	if strings.Count(strings.ToUpper(reportSource), "CONTENT_REPORTS") != 1 {
		t.Fatalf("report.go references content_reports more than once; only the insert is permitted:\n%s",
			reportSource)
	}
	adminReportSource := sources[filepath.Join("internal", "learning", "report_admin.go")]
	if !strings.Contains(adminReportSource, "UPDATE content_reports") || !strings.Contains(adminReportSource, "resolved_at") {
		t.Fatal("report_admin.go no longer owns the terminal resolution update")
	}
}

// TestNoLifecyclePackageIsReachableFromReporting proves the separation structurally: the packages
// that can delist, retire, and suspend do not know reports exist, so no report can reach them.
func TestNoLifecyclePackageIsReachableFromReporting(t *testing.T) {
	sources := productionSources(t)

	reportSymbols := []string{"content_reports", "CreateReport", "ReportTargetKind", "VerifiedReportBinding", "ContentReport"}
	lifecyclePackages := []string{
		filepath.Join("internal", "catalog") + string(filepath.Separator),
		filepath.Join("internal", "media") + string(filepath.Separator),
		filepath.Join("internal", "identity") + string(filepath.Separator),
		filepath.Join("internal", "entitlement") + string(filepath.Separator),
	}

	for file, source := range sources {
		inLifecycle := false
		for _, pkg := range lifecyclePackages {
			if strings.HasPrefix(file, pkg) {
				inLifecycle = true
				break
			}
		}
		if !inLifecycle {
			continue
		}
		for _, symbol := range reportSymbols {
			if strings.Contains(source, symbol) {
				t.Fatalf("%s references %q; a lifecycle package must not be reachable from reporting", file, symbol)
			}
		}
	}

	// And the reporting domain does not reach back out to moderate anything.
	reportSource := sources[filepath.Join("internal", "learning", "report.go")]
	for _, forbidden := range []string{
		"UPDATE courses", "UPDATE course_revisions", "UPDATE course_lessons", "UPDATE course_sections",
		"UPDATE media_asset_versions", "UPDATE media_assets", "UPDATE accounts", "UPDATE entitlements",
		"UPDATE enrollments", "UPDATE lesson_files", "DELETE FROM",
	} {
		if strings.Contains(strings.ToUpper(reportSource), strings.ToUpper(forbidden)) {
			t.Fatalf("report creation contains %q; a report moderates nothing", forbidden)
		}
	}
}

// TestReportRouteAnswersOnlyPost pins the router's own contract at the one report path: the
// submission method is mounted and every other method is refused by the shared handler, not by an
// S5-specific one invented for moderation verbs.
func TestReportRouteAnswersOnlyPost(t *testing.T) {
	router, _ := authzRouter(t, fixedPrincipals{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		mounted := false
		for _, route := range router.Routes() {
			if route.Path == "/api/v1/learn/reports" && route.Method == method {
				mounted = true
			}
		}
		if mounted {
			t.Fatalf("%s /api/v1/learn/reports is mounted; the report route only creates", method)
		}
	}
}
