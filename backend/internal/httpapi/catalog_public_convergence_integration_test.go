//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	publicCatalogueLaunchCourseCount = 12
	publicSearchLaunchBudget         = 2500 * time.Millisecond
	publicNotFoundTimingSamples      = 100
	publicNotFoundP95Tolerance       = 25 * time.Millisecond
)

func TestPublicCatalogQueryPlansAtLaunchScale(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	var detailCourseID string
	for range publicCatalogueLaunchCourseCount {
		courseID := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
		if detailCourseID == "" {
			detailCourseID = courseID
		}
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring query-plan connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("preferring catalogue indexes: %v", err)
	}
	visibility := catalogpublic.PublishedOnly("c", "cr")
	requirePublicCatalogPlanIndex(t, conn, ctx, `
		SELECT c.id
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE `+visibility+`
		ORDER BY c.id
		LIMIT 20`, "courses_pkey")
	requirePublicCatalogPlanIndex(t, conn, ctx, `
		SELECT c.id
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE `+visibility+` AND c.id = $1::uuid`, "courses_pkey", detailCourseID)

	plan := publicCatalogExplain(t, conn, ctx, `
		EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF)
		SELECT c.id
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		JOIN accounts a ON a.id = c.owner_account_id
		LEFT JOIN taxonomy_terms major ON major.id = cr.major_term_id
		LEFT JOIN taxonomy_terms subject ON subject.id = cr.subject_term_id
		WHERE `+visibility+`
			AND catalog_normalize_ar(concat_ws(' ', a.display_name, major.label_ar, major.label_en,
				major.academic_code, subject.label_ar, subject.label_en, subject.academic_code,
				cr.study_year::text)) LIKE '%' || catalog_normalize_ar($1) || '%'`, "public owner")
	execution := publicCatalogPlanExecutionTime(t, plan)
	t.Logf("unindexed joined-field search at %d Courses: %s", publicCatalogueLaunchCourseCount, execution)
	if execution > publicSearchLaunchBudget {
		t.Fatalf("unindexed joined-field search = %s, exceeds %s launch budget", execution, publicSearchLaunchBudget)
	}
}

func TestPublicCatalogNotFoundTimingDistribution(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	hiddenCourseID := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DRAFT"})
	router := buildPublicCatalogRouter(t, pool)
	hiddenPath := "/api/v1/catalog/courses/" + hiddenCourseID
	missingPath := "/api/v1/catalog/courses/00000000-0000-0000-0000-000000000000"

	for range 10 {
		assertPublicCatalogNotFound(t, router, hiddenPath)
		assertPublicCatalogNotFound(t, router, missingPath)
	}
	hiddenSamples := make([]time.Duration, 0, publicNotFoundTimingSamples)
	missingSamples := make([]time.Duration, 0, publicNotFoundTimingSamples)
	for range publicNotFoundTimingSamples {
		hiddenSamples = append(hiddenSamples, publicCatalogLookupDuration(t, router, hiddenPath))
		missingSamples = append(missingSamples, publicCatalogLookupDuration(t, router, missingPath))
	}
	hiddenP95 := publicCatalogPercentile(hiddenSamples, 95)
	missingP95 := publicCatalogPercentile(missingSamples, 95)
	delta := hiddenP95 - missingP95
	if delta < 0 {
		delta = -delta
	}
	t.Logf("not-found timing samples=%d hidden-p95=%s absent-p95=%s delta=%s tolerance=%s", publicNotFoundTimingSamples, hiddenP95, missingP95, delta, publicNotFoundP95Tolerance)
	if delta > publicNotFoundP95Tolerance {
		t.Fatalf("hidden/absent p95 delta = %s, exceeds tolerance %s", delta, publicNotFoundP95Tolerance)
	}
}

func requirePublicCatalogPlanIndex(t *testing.T, conn *pgxpool.Conn, ctx context.Context, query, index string, arguments ...any) {
	t.Helper()
	plan := publicCatalogExplain(t, conn, ctx, "EXPLAIN (COSTS OFF) "+query, arguments...)
	if !strings.Contains(plan, index) {
		t.Fatalf("catalogue query plan omitted %s:\n%s", index, plan)
	}
}

func publicCatalogExplain(t *testing.T, conn *pgxpool.Conn, ctx context.Context, query string, arguments ...any) string {
	t.Helper()
	rows, err := conn.Query(ctx, query, arguments...)
	if err != nil {
		t.Fatalf("explaining public catalogue query: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scanning public catalogue plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading public catalogue plan: %v", err)
	}
	return strings.Join(plan, "\n")
}

func publicCatalogPlanExecutionTime(t *testing.T, plan string) time.Duration {
	t.Helper()
	for _, line := range strings.Split(plan, "\n") {
		if !strings.HasPrefix(line, "Execution Time:") {
			continue
		}
		milliseconds, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "Execution Time:")), " ms"), 64)
		if err != nil {
			t.Fatalf("parsing public catalogue execution time %q: %v", line, err)
		}
		return time.Duration(milliseconds * float64(time.Millisecond))
	}
	t.Fatalf("public catalogue plan omitted execution time:\n%s", plan)
	return 0
}

func assertPublicCatalogNotFound(t *testing.T, router *gin.Engine, path string) {
	t.Helper()
	recorder := publicCatalogRequest(router, http.MethodGet, path)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public lookup %s = %d, want 404", path, recorder.Code)
	}
}

func publicCatalogLookupDuration(t *testing.T, router *gin.Engine, path string) time.Duration {
	t.Helper()
	started := time.Now()
	assertPublicCatalogNotFound(t, router, path)
	return time.Since(started)
}

func publicCatalogPercentile(samples []time.Duration, percentile int) time.Duration {
	sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
	index := (len(samples)*percentile + 99) / 100
	return samples[index-1]
}
