package learning

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// T071: D-046 absence over the production build.
//
// The external Discord/Telegram Course community link left MVP scope and is deferred to S18. No
// slice authors it, stores it, serves it, or renders it before launch — so the honest state of S5
// is that the concept is not present at all, not that it is present and hidden.
//
// That distinction is why this is an absence *detector* rather than an inspection. "We looked and
// there was nothing" decays the moment someone adds a column; a test that fails when the concept
// appears keeps the decision enforced. Mutation row 16 of the required-mutations table exists to
// prove this detector is not vacuous: adding `community_link` to the S5 migration must turn it red.
//
// The frontend halves of D-046 — `frontend/src/app/[locale]/learn/` and
// `frontend/src/components/learning/` — are asserted in the frontend suite, where those files live.

// communityConcepts is the forbidden set. `community`, `discord`, and `telegram` are the three T071
// names verbatim; the rest are the same capability under another name, because a rename is the
// obvious way this decision would erode. Each is matched case-insensitively against text with
// separators removed, so `community_link`, `communityUrl`, `CommunityLink`, `community-link`, and
// `community link` are one concept rather than five spellings.
var communityConcepts = []string{
	"community",
	"discord",
	"telegram",
	"whatsapp",
	"groupchat",
	"discussiongroup",
	"socialgroup",
	"sociallink",
	"joinourgroup",
	"comingsoon",
}

// separators are removed before matching so no casing or delimiter style escapes the scan. Letters
// and digits survive; everything else is dropped.
var separators = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeForConceptScan(source string) string {
	return separators.ReplaceAllString(strings.ToLower(source), "")
}

// t071Root is one authoritative scan root and the file kinds it owns.
type t071Root struct {
	relative   string
	extensions []string
	// productionOnly drops `_test.go`: T071 is about the production build, and a test that names a
	// concept in order to forbid it must not be mistaken for the concept being present.
	productionOnly bool
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the backend root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

// collectT071Files walks one root and returns the files it owns.
func collectT071Files(t *testing.T, base string, root t071Root) []string {
	t.Helper()
	full := filepath.Join(base, root.relative)
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		// A silently vanished root would make this test pass by scanning nothing.
		t.Fatalf("T071 scan root %s is missing; the detector would pass vacuously", root.relative)
	}
	var files []string
	err = filepath.Walk(full, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if root.productionOnly && strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, extension := range root.extensions {
			if strings.HasSuffix(path, extension) {
				files = append(files, path)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root.relative, err)
	}
	sort.Strings(files)
	return files
}

// s5MigrationRoot covers both S5 migrations, up and down. Row 16 mutates one of these files, so the
// scan must reach the SQL itself rather than a schema summary of it.
func t071BackendRoots() []t071Root {
	return []t071Root{
		{relative: "internal/learning", extensions: []string{".go"}, productionOnly: true},
		{relative: "internal/db/migrations", extensions: []string{".sql"}},
	}
}

// TestNoCommunitySurfaceInProtectedLearningOrItsMigrations is T071's backend half.
func TestNoCommunitySurfaceInProtectedLearningOrItsMigrations(t *testing.T) {
	base := backendRoot(t)

	scanned := 0
	for _, root := range t071BackendRoots() {
		files := collectT071Files(t, base, root)
		if len(files) == 0 {
			t.Fatalf("T071 scan root %s matched no files; the detector would pass vacuously", root.relative)
		}
		scanned += len(files)

		for _, path := range files {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			// String literals, SQL identifiers, object keys, and route paths are all in scope —
			// only the delimiters are normalized away, never the content.
			normalized := normalizeForConceptScan(string(content))
			relative, relErr := filepath.Rel(base, path)
			if relErr != nil {
				relative = path
			}
			for _, concept := range communityConcepts {
				if strings.Contains(normalized, concept) {
					t.Fatalf("%s carries the deferred community concept %q; D-046 defers it to S18, "+
						"so S5 stores, serves, and renders none of it", relative, concept)
				}
			}
		}
	}

	// The S5 migrations must actually be among the files scanned, or row 16 could not be detected.
	migrations := collectT071Files(t, base, t071Root{relative: "internal/db/migrations", extensions: []string{".sql"}})
	var sawS5 bool
	for _, path := range migrations {
		if strings.Contains(filepath.Base(path), "0014_protected_learning") {
			sawS5 = true
		}
	}
	if !sawS5 {
		t.Fatal("the S5 protected-learning migration was not scanned; mutation row 16 could not be detected")
	}
	if scanned < 2 {
		t.Fatalf("T071 scanned only %d files", scanned)
	}
	t.Logf("T071 backend scan covered %d production files across %d roots", scanned, len(t071BackendRoots()))
}

// TestCommunityConceptScanRecognisesEverySpelling proves the normalization actually collapses the
// casing and delimiter variants a rename would use — the detector's own non-vacuity, independent of
// the mutation run.
func TestCommunityConceptScanRecognisesEverySpelling(t *testing.T) {
	for _, spelling := range []string{
		"community_link",
		"communityLink",
		"CommunityURL",
		"community-url",
		"community link",
		"COMMUNITY_LINK",
		"ALTER TABLE courses ADD COLUMN community_link TEXT;",
		"discordInvite",
		"telegram_group",
		"Join our WhatsApp group",
		"groupChat",
		"discussion-group",
		"social_group",
		"Coming soon",
	} {
		normalized := normalizeForConceptScan(spelling)
		matched := false
		for _, concept := range communityConcepts {
			if strings.Contains(normalized, concept) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("the concept scan missed %q (normalized %q)", spelling, normalized)
		}
	}

	// And it does not fire on ordinary text, so the detector stays usable.
	for _, benign := range []string{
		"lessonIdentity", "reportContext", "entitlementEvaluator",
		"CREATE TABLE content_reports", "commit the transaction", "communication is not the word",
	} {
		normalized := normalizeForConceptScan(benign)
		for _, concept := range communityConcepts {
			if strings.Contains(normalized, concept) {
				t.Fatalf("the concept scan false-positived on %q via %q", benign, concept)
			}
		}
	}
}
