//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// T069 against real PostgreSQL and the production router (FR-035, BR-146).
//
// The inventory tests prove no moderation route is mounted. These prove what that means for a
// report that already exists: every shape a moderation request could take is refused by the
// router's own unknown-route contract, and the report stays exactly as it was written — unresolved,
// unedited, undeleted — with its target and the Student's access untouched.

// storedReportRow is the whole row, so a change to any field is visible rather than inferred from a
// count that would not move under an in-place edit.
type storedReportRow struct {
	id          string
	reporter    string
	kind        string
	target      string
	revisionRef string
	reason      string
	explanation *string
	resolvedAt  *time.Time
	createdAt   time.Time
}

func readWholeReportRows(t *testing.T, f learningIntegrationFixture) []storedReportRow {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT id::text, reporter_account_id::text, target_kind::text, target_id::text,
		       target_revision_ref::text, reason::text, explanation, resolved_at, created_at
		FROM content_reports ORDER BY id
	`)
	if err != nil {
		t.Fatalf("reading reports: %v", err)
	}
	defer rows.Close()
	var stored []storedReportRow
	for rows.Next() {
		var row storedReportRow
		if err := rows.Scan(&row.id, &row.reporter, &row.kind, &row.target, &row.revisionRef,
			&row.reason, &row.explanation, &row.resolvedAt, &row.createdAt); err != nil {
			t.Fatalf("scanning report: %v", err)
		}
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating reports: %v", err)
	}
	return stored
}

// forbiddenModerationRequests enumerates every shape a moderation call could plausibly take against
// an existing report, including the paths a future S8 might one day use.
func forbiddenModerationRequests(reportID string) []struct{ method, path string } {
	base := "/api/v1/learn/reports"
	return []struct{ method, path string }{
		// Reading or listing the queue.
		{http.MethodGet, base},
		{http.MethodGet, base + "/" + reportID},
		// Editing the report in place.
		{http.MethodPatch, base + "/" + reportID},
		{http.MethodPut, base + "/" + reportID},
		{http.MethodDelete, base + "/" + reportID},
		{http.MethodPatch, base},
		{http.MethodPut, base},
		{http.MethodDelete, base},
		// Acting on it.
		{http.MethodPost, base + "/" + reportID + "/resolve"},
		{http.MethodPost, base + "/" + reportID + "/dismiss"},
		{http.MethodPost, base + "/" + reportID + "/close"},
		{http.MethodPost, base + "/" + reportID + "/reopen"},
		{http.MethodPost, base + "/" + reportID + "/moderate"},
		{http.MethodPost, base + "/" + reportID + "/assign"},
		{http.MethodPost, base + "/" + reportID + "/delist"},
		{http.MethodPost, base + "/" + reportID + "/retire"},
		{http.MethodPost, base + "/" + reportID + "/suspend"},
		// Admin and moderation namespaces S5 must not have opened.
		{http.MethodGet, "/api/v1/admin/reports"},
		{http.MethodGet, "/api/v1/admin/reports/" + reportID},
		{http.MethodPost, "/api/v1/admin/reports/" + reportID + "/resolve"},
		{http.MethodGet, "/api/v1/moderation/reports"},
		{http.MethodPost, "/api/v1/moderation/reports/" + reportID + "/dismiss"},
		{http.MethodGet, "/api/v1/learn/reports/queue"},
		{http.MethodGet, "/api/v1/learn/moderation"},
	}
}

// TestNoRequestCanResolveDismissOrRemoveAReport is T069's behavioural half.
func TestNoRequestCanResolveDismissOrRemoveAReport(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	attachLessonMaterial(t, f, "RESOURCE", "t069-resource")

	// One real, unresolved report to attempt moderation against.
	if response := submitReport(f, courseContextOf(t, f), "inappropriate"); response.Code != http.StatusCreated {
		t.Fatalf("seeding a report = %d %s", response.Code, response.Body.String())
	}
	reportsBefore := readWholeReportRows(t, f)
	if len(reportsBefore) != 1 {
		t.Fatalf("expected exactly one seeded report, got %d", len(reportsBefore))
	}
	if reportsBefore[0].resolvedAt != nil {
		t.Fatal("a newly created report is already resolved")
	}
	reportID := reportsBefore[0].id

	contentBefore := t068ContentSnapshot(t, f)
	authorityBefore := f.authoritySnapshot(t)

	for _, attempt := range forbiddenModerationRequests(reportID) {
		response := f.request(attempt.method, attempt.path, "")

		// The router's own contract: an unmounted path is 404, and a mounted path reached with an
		// unmounted method is 405. Either way no handler ran — what must never happen is a 2xx.
		switch response.Code {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
		default:
			t.Fatalf("%s %s = %d %s; no moderation request may be handled",
				attempt.method, attempt.path, response.Code, response.Body.String())
		}
		if response.Code >= 200 && response.Code < 300 {
			t.Fatalf("%s %s succeeded", attempt.method, attempt.path)
		}
		// A refusal must not describe the report either.
		if body := response.Body.String(); len(body) > 0 {
			for _, secret := range []string{reportID, reportsBefore[0].revisionRef, reportsBefore[0].target} {
				if secret != "" && strings.Contains(body, secret) {
					t.Fatalf("%s %s leaked %q", attempt.method, attempt.path, secret)
				}
			}
		}
	}

	// The report is byte-identical: still unresolved, still present, still unedited.
	reportsAfter := readWholeReportRows(t, f)
	if len(reportsAfter) != len(reportsBefore) {
		t.Fatalf("report rows changed from %d to %d", len(reportsBefore), len(reportsAfter))
	}
	for index, before := range reportsBefore {
		after := reportsAfter[index]
		if after != before {
			t.Fatalf("a moderation attempt changed the report:\nbefore %+v\nafter  %+v", before, after)
		}
		if after.resolvedAt != nil {
			t.Fatal("resolved_at was set; resolution is S8's (FR-035)")
		}
	}

	// The target, the Course graph, and the Student's authority are equally untouched.
	assertContentUnchanged(t, "moderation attempts", contentBefore, t068ContentSnapshot(t, f))
	if after := f.authoritySnapshot(t); after != authorityBefore {
		t.Fatalf("moderation attempts changed authority:\nbefore %+v\nafter  %+v", authorityBefore, after)
	}
}

// TestSubmissionCreatesAnUnresolvedReportAndNothingElse connects the inventory to the one route
// that does exist: it creates, and creation is not a moderation decision.
//
// Two reports exist by the end on purpose. A single-report fixture cannot see a submission that
// resolves *other* reports — the "one open report at a time, so close the last one" instinct — and
// that is exactly the kind of quiet moderation FR-035 forbids.
func TestSubmissionCreatesAnUnresolvedReportAndNothingElse(t *testing.T) {
	f := newLearningIntegrationFixture(t)

	response := submitReport(f, courseContextOf(t, f), "suspected_copyright_violation")
	if response.Code != http.StatusCreated {
		t.Fatalf("submission = %d %s", response.Code, response.Body.String())
	}

	stored := readWholeReportRows(t, f)
	if len(stored) != 1 {
		t.Fatalf("stored reports = %d, want 1", len(stored))
	}
	if stored[0].resolvedAt != nil {
		t.Fatal("submission set resolved_at; a report is created unresolved and stays that way in S5")
	}
	first := stored[0]

	// A second report, on a different target kind so D-066 admits it. Creating it must not resolve,
	// close, or otherwise disturb the first.
	if second := submitReport(f, lessonContextsOf(t, f).Lesson, "inaccurate"); second.Code != http.StatusCreated {
		t.Fatalf("second submission = %d %s", second.Code, second.Body.String())
	}
	afterSecond := readWholeReportRows(t, f)
	if len(afterSecond) != 2 {
		t.Fatalf("stored reports = %d, want 2", len(afterSecond))
	}
	for _, row := range afterSecond {
		if row.resolvedAt != nil {
			t.Fatalf("report %s was resolved by a later submission; S5 resolves nothing", row.id)
		}
		if row.id == first.id && row != first {
			t.Fatalf("the earlier report changed when a second was filed:\nbefore %+v\nafter  %+v", first, row)
		}
	}

	// The acknowledgement offers no moderation affordance — no status, no action, no link.
	body := response.Body.String()
	for _, forbidden := range []string{
		"resolve", "dismiss", "close", "moderat", "queue", "status", "review", "action", "href", "link",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the acknowledgement exposed %q: %s", forbidden, body)
		}
	}

	// And the report stays unresolved across subsequent protected reads — nothing in S5 sweeps it.
	if home := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, ""); home.Code != http.StatusOK {
		t.Fatalf("course home after reporting = %d", home.Code)
	}
	for _, row := range readWholeReportRows(t, f) {
		if row.resolvedAt != nil {
			t.Fatalf("a later read resolved report %s", row.id)
		}
	}
}
