//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// D-102 §"Revision boundary". Reordering writes positions into one editable
// candidate. A Student reads the live revision, so the order they see must not
// move until the existing publication flow promotes that exact candidate — and
// once it does, the Lesson Player's Previous/Next (D-099) must walk the newly
// live order rather than a remembered one.

type d102RevisionSection struct {
	rowID, identityID, titleAr, titleEn string
	position                            int
}

// reversedCandidateRevision builds a non-live candidate carrying the same stable
// identities as the live revision with every Section and Lesson position
// reversed. It is the persisted shape a reorder leaves behind, written directly
// so this test isolates the read boundary; the reorder command that produces it
// is covered by TestD102SectionReorderingExactSetAndAtomicity and
// TestD102CandidateOrderIsolatedUntilNormalPublication in internal/catalog.
func reversedCandidateRevision(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	ctx := context.Background()
	var liveID string
	if err := f.pool.QueryRow(ctx, `SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&liveID); err != nil {
		t.Fatalf("reading live revision: %v", err)
	}
	candidateID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 2, 'مرشح', 'Candidate')`, candidateID, f.courseID); err != nil {
		t.Fatalf("creating candidate revision: %v", err)
	}

	rows, err := f.pool.Query(ctx, `SELECT id::text, section_identity_id::text, title_ar, title_en, position FROM course_sections WHERE revision_id = $1::uuid ORDER BY position, id`, liveID)
	if err != nil {
		t.Fatalf("reading live sections: %v", err)
	}
	sections := make([]d102RevisionSection, 0)
	for rows.Next() {
		var section d102RevisionSection
		if err := rows.Scan(&section.rowID, &section.identityID, &section.titleAr, &section.titleEn, &section.position); err != nil {
			t.Fatalf("scanning live section: %v", err)
		}
		sections = append(sections, section)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("reading live sections: %v", err)
	}

	for index, section := range sections {
		candidateSectionRow := uuid.NewString()
		if _, err := f.pool.Exec(ctx, `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)`,
			candidateSectionRow, candidateID, f.courseID, section.identityID, section.titleAr, section.titleEn, len(sections)-1-index); err != nil {
			t.Fatalf("copying section into candidate: %v", err)
		}
		lessonRows, err := f.pool.Query(ctx, `SELECT lesson_identity_id::text, title_ar, title_en, video_asset_version_id::text FROM course_lessons WHERE section_id = $1::uuid ORDER BY position, id`, section.rowID)
		if err != nil {
			t.Fatalf("reading live lessons: %v", err)
		}
		type liveLesson struct {
			identityID, titleAr, titleEn string
			versionID                    *string
		}
		lessons := make([]liveLesson, 0)
		for lessonRows.Next() {
			var lesson liveLesson
			if err := lessonRows.Scan(&lesson.identityID, &lesson.titleAr, &lesson.titleEn, &lesson.versionID); err != nil {
				t.Fatalf("scanning live lesson: %v", err)
			}
			lessons = append(lessons, lesson)
		}
		lessonRows.Close()
		if err := lessonRows.Err(); err != nil {
			t.Fatalf("reading live lessons: %v", err)
		}
		for lessonIndex, lesson := range lessons {
			if _, err := f.pool.Exec(ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8, $9::uuid)`,
				uuid.NewString(), candidateSectionRow, f.courseID, section.identityID, lesson.identityID, lesson.titleAr, lesson.titleEn, len(lessons)-1-lessonIndex, lesson.versionID); err != nil {
				t.Fatalf("copying lesson into candidate: %v", err)
			}
		}
	}
	return candidateID
}

func d102StudentLessonOrder(t *testing.T, f learningIntegrationFixture) []string {
	t.Helper()
	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "", map[string]string{"Accept-Language": "en"})
	if response.Code != http.StatusOK {
		t.Fatalf("Course Home = %d %s", response.Code, response.Body.String())
	}
	var home struct {
		Sections []struct {
			Lessons []struct {
				LessonID string `json:"lesson_id"`
			} `json:"lessons"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &home); err != nil {
		t.Fatalf("decoding Course Home: %v", err)
	}
	order := make([]string, 0)
	for _, section := range home.Sections {
		for _, lesson := range section.Lessons {
			order = append(order, lesson.LessonID)
		}
	}
	return order
}

func d102StudentNavigation(t *testing.T, f learningIntegrationFixture, lessonID string) (previous, next string) {
	t.Helper()
	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+lessonID, "", map[string]string{"Accept-Language": "en"})
	if response.Code != http.StatusOK {
		t.Fatalf("Lesson read = %d %s", response.Code, response.Body.String())
	}
	var lesson struct {
		Navigation struct {
			Previous *string `json:"previous_lesson_id"`
			Next     *string `json:"next_lesson_id"`
		} `json:"navigation"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &lesson); err != nil {
		t.Fatalf("decoding Lesson: %v", err)
	}
	if lesson.Navigation.Previous != nil {
		previous = *lesson.Navigation.Previous
	}
	if lesson.Navigation.Next != nil {
		next = *lesson.Navigation.Next
	}
	return previous, next
}

func TestD102StudentOrderChangesOnlyWhenTheReorderedRevisionGoesLive(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	second := addLearningSectionLesson(t, f, 1, "two")
	third := addLearningSectionLesson(t, f, 2, "three")
	live := []string{f.lessonID, second, third}

	if got := d102StudentLessonOrder(t, f); !reflect.DeepEqual(got, live) {
		t.Fatalf("live order = %v, want %v", got, live)
	}
	if previous, next := d102StudentNavigation(t, f, f.lessonID); previous != "" || next != second {
		t.Fatalf("first Lesson navigation = (%q, %q), want (\"\", %q)", previous, next, second)
	}
	if previous, next := d102StudentNavigation(t, f, second); previous != f.lessonID || next != third {
		t.Fatalf("middle Lesson navigation = (%q, %q), want (%q, %q)", previous, next, f.lessonID, third)
	}

	candidateID := reversedCandidateRevision(t, f)
	if got := d102StudentLessonOrder(t, f); !reflect.DeepEqual(got, live) {
		t.Fatalf("Student order moved while the reordered revision was still a candidate: %v", got)
	}
	if previous, next := d102StudentNavigation(t, f, f.lessonID); previous != "" || next != second {
		t.Fatalf("Previous/Next followed the candidate before publication: (%q, %q)", previous, next)
	}

	// What publication does to these two rows: the candidate becomes APPROVED and
	// the Course points at it. The read path refuses a live revision that is not
	// APPROVED, so both halves are required.
	if _, err := f.pool.Exec(context.Background(), `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, candidateID); err != nil {
		t.Fatalf("approving the candidate: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, candidateID, f.courseID); err != nil {
		t.Fatalf("promoting the candidate: %v", err)
	}

	promoted := []string{third, second, f.lessonID}
	if got := d102StudentLessonOrder(t, f); !reflect.DeepEqual(got, promoted) {
		t.Fatalf("published order = %v, want %v", got, promoted)
	}
	if previous, next := d102StudentNavigation(t, f, third); previous != "" || next != second {
		t.Fatalf("first Lesson navigation after publication = (%q, %q), want (\"\", %q)", previous, next, second)
	}
	if previous, next := d102StudentNavigation(t, f, f.lessonID); previous != second || next != "" {
		t.Fatalf("last Lesson navigation after publication = (%q, %q), want (%q, \"\")", previous, next, second)
	}
}
