//go:build integration

package catalog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type reorderFixture struct {
	pool       *pgxpool.Pool
	repo       *Repository
	ctx        context.Context
	ownerID    string
	courseID   string
	revisionID string
	sections   []*Section
}

func newReorderFixture(t *testing.T) *reorderFixture {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)
	ownerID, courseID := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	var revisionID string
	if err := p.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("querying revision: %v", err)
	}
	f := &reorderFixture{pool: p, repo: repo, ctx: ctx, ownerID: ownerID, courseID: courseID, revisionID: revisionID}
	for i, title := range []string{"A", "B", "C"} {
		position := i * 3 // prove sparse input is normalized by an explicit reorder
		section, err := repo.AddSection(ctx, AddSectionRequest{
			CourseID: courseID, RevisionID: revisionID, OwnerAccountID: ownerID,
			TitleAr: title, TitleEn: title, Position: &position,
		}, ownerID)
		if err != nil {
			t.Fatalf("AddSection(%s): %v", title, err)
		}
		f.sections = append(f.sections, section)
	}
	for i, title := range []string{"L1", "L2", "L3"} {
		position := i * 4
		if _, err := repo.AddLesson(ctx, AddLessonRequest{
			CourseID: courseID, RevisionID: revisionID, SectionID: f.sections[0].SectionIdentityID,
			OwnerAccountID: ownerID, TitleAr: title, TitleEn: title, Position: &position,
		}, ownerID); err != nil {
			t.Fatalf("AddLesson(%s): %v", title, err)
		}
	}
	return f
}

func sectionIDs(revision *CourseRevision) []string {
	ids := make([]string, len(revision.Sections))
	for i := range revision.Sections {
		ids[i] = revision.Sections[i].SectionIdentityID
	}
	return ids
}

func lessonIDs(section Section) []string {
	ids := make([]string, len(section.Lessons))
	for i := range section.Lessons {
		ids[i] = section.Lessons[i].LessonIdentityID
	}
	return ids
}

func assertDenseSectionPositions(t *testing.T, revision *CourseRevision) {
	t.Helper()
	for i, section := range revision.Sections {
		if section.Position != i {
			t.Fatalf("section %s position = %d, want %d", section.SectionIdentityID, section.Position, i)
		}
	}
}

func assertDenseLessonPositions(t *testing.T, section Section) {
	t.Helper()
	for i, lesson := range section.Lessons {
		if lesson.Position != i {
			t.Fatalf("lesson %s position = %d, want %d", lesson.LessonIdentityID, lesson.Position, i)
		}
	}
}

func TestD102SectionReorderingExactSetAndAtomicity(t *testing.T) {
	f := newReorderFixture(t)
	original, err := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := original.Sections[0].SectionIdentityID, original.Sections[1].SectionIdentityID, original.Sections[2].SectionIdentityID

	for name, requested := range map[string][]string{
		"duplicate": {a, a, c},
		"missing":   {a, b},
		"foreign":   {a, b, "99999999-9999-9999-9999-999999999999"},
	} {
		t.Run(name+" rejected without partial write", func(t *testing.T) {
			_, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
				CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.ownerID, SectionIDs: requested,
			}, f.ownerID)
			if !errors.Is(err, ErrInvalidOrder) {
				t.Fatalf("error = %v, want ErrInvalidOrder", err)
			}
			after, loadErr := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !reflect.DeepEqual(sectionIDs(after), []string{a, b, c}) {
				t.Fatalf("order changed after rejected request: %v", sectionIDs(after))
			}
		})
	}

	reversed, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.ownerID, SectionIDs: []string{c, b, a},
	}, f.ownerID)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if !reflect.DeepEqual(sectionIDs(reversed), []string{c, b, a}) {
		t.Fatalf("reverse order = %v", sectionIDs(reversed))
	}
	assertDenseSectionPositions(t, reversed)

	noOp, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.ownerID, SectionIDs: []string{c, b, a},
	}, f.ownerID)
	if err != nil {
		t.Fatalf("no-op: %v", err)
	}
	assertDenseSectionPositions(t, noOp)

	otherOwner := "33333333-3333-3333-3333-333333333333"
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1, 'other@example.com', 'other@example.com', 'INSTRUCTOR', 'ACTIVE', 'Other')`, otherOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: otherOwner, SectionIDs: []string{c, b, a},
	}, otherOwner); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("non-owner error = %v, want ErrCourseNotFound", err)
	}

	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1::uuid`, f.revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.ownerID, SectionIDs: []string{a, b, c},
	}, f.ownerID); err == nil {
		t.Fatal("PENDING_REVIEW reorder succeeded")
	} else {
		var conflict *LifecycleConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("PENDING_REVIEW error = %v, want lifecycle conflict", err)
		}
	}
}

func TestD102LessonReorderingStaysInsideSection(t *testing.T) {
	f := newReorderFixture(t)
	revision, err := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
	if err != nil {
		t.Fatal(err)
	}
	first := revision.Sections[0]
	l1, l2, l3 := first.Lessons[0].LessonIdentityID, first.Lessons[1].LessonIdentityID, first.Lessons[2].LessonIdentityID
	sibling, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, SectionID: f.sections[1].SectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "Sibling", TitleEn: "Sibling",
	}, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}

	for name, requested := range map[string][]string{
		"duplicate":       {l1, l1, l3},
		"missing":         {l1, l2},
		"sibling section": {l1, l2, sibling.LessonIdentityID},
		"foreign":         {l1, l2, "99999999-9999-9999-9999-999999999999"},
	} {
		t.Run(name+" rejected atomically", func(t *testing.T) {
			_, err := f.repo.ReorderLessons(f.ctx, ReorderLessonsRequest{
				CourseID: f.courseID, RevisionID: f.revisionID, SectionID: first.SectionIdentityID,
				OwnerAccountID: f.ownerID, LessonIDs: requested,
			}, f.ownerID)
			if !errors.Is(err, ErrInvalidOrder) {
				t.Fatalf("error = %v, want ErrInvalidOrder", err)
			}
			after, loadErr := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !reflect.DeepEqual(lessonIDs(after.Sections[0]), []string{l1, l2, l3}) {
				t.Fatalf("lesson order changed after rejected request: %v", lessonIDs(after.Sections[0]))
			}
		})
	}

	canonical, err := f.repo.ReorderLessons(f.ctx, ReorderLessonsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, SectionID: first.SectionIdentityID,
		OwnerAccountID: f.ownerID, LessonIDs: []string{l3, l1, l2},
	}, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lessonIDs(canonical.Sections[0]), []string{l3, l1, l2}) {
		t.Fatalf("lesson order = %v", lessonIDs(canonical.Sections[0]))
	}
	assertDenseLessonPositions(t, canonical.Sections[0])
	if _, err := f.repo.ReorderLessons(f.ctx, ReorderLessonsRequest{
		CourseID: f.courseID, RevisionID: f.revisionID, SectionID: first.SectionIdentityID,
		OwnerAccountID: f.ownerID, LessonIDs: []string{l3, l1, l2},
	}, f.ownerID); err != nil {
		t.Fatalf("no-op: %v", err)
	}
}

func TestD102ConcurrentReordersRemainCanonical(t *testing.T) {
	f := newReorderFixture(t)
	revision, err := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := revision.Sections[0].SectionIdentityID, revision.Sections[1].SectionIdentityID, revision.Sections[2].SectionIdentityID
	orders := [][]string{{c, b, a}, {b, a, c}}
	errs := make(chan error, len(orders))
	var start sync.WaitGroup
	start.Add(1)
	for _, order := range orders {
		order := order
		go func() {
			start.Wait()
			_, err := f.repo.ReorderSections(f.ctx, ReorderSectionsRequest{
				CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.ownerID, SectionIDs: order,
			}, f.ownerID)
			errs <- err
		}()
	}
	start.Done()
	for range orders {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent reorder: %v", err)
		}
	}
	final, err := f.repo.loadRevisionGraphByID(f.ctx, f.revisionID)
	if err != nil {
		t.Fatal(err)
	}
	assertDenseSectionPositions(t, final)
	got := sectionIDs(final)
	if !reflect.DeepEqual(got, orders[0]) && !reflect.DeepEqual(got, orders[1]) {
		t.Fatalf("final order is not a submitted permutation: %v", got)
	}
}

func TestD102CandidateOrderIsolatedUntilNormalPublication(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	second, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "الدرس الثاني", TitleEn: "SECOND",
	}, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "الدرس الثالث", TitleEn: "THIRD",
	}, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, lessonID := range []string{second.LessonIdentityID, third.LessonIdentityID} {
		if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: lessonID,
			VideoAssetVersionID: f.videoNew, OwnerAccountID: f.ownerID,
		}, f.ownerID); err != nil {
			t.Fatalf("SetLessonVideo(%s): %v", lessonID, err)
		}
	}
	desired := []string{third.LessonIdentityID, f.lessonIdentityID, second.LessonIdentityID}
	if _, err := f.repo.ReorderLessons(f.ctx, ReorderLessonsRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionIdentityID,
		OwnerAccountID: f.ownerID, LessonIDs: desired,
	}, f.ownerID); err != nil {
		t.Fatal(err)
	}

	review, err := f.repo.GetCourseRevisionGraph(f.ctx, f.courseID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := lessonIDs(review.EditableRevision.Sections[0]); !reflect.DeepEqual(got, desired) {
		t.Fatalf("Admin exact candidate order = %v, want %v", got, desired)
	}
	if got := lessonIDs(review.LiveRevision.Sections[0]); !reflect.DeepEqual(got, []string{f.lessonIdentityID}) {
		t.Fatalf("live order changed before publication: %v", got)
	}

	published, err := f.repo.PublishRevision(f.ctx, f.validator, PublishRevisionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	})
	if err != nil {
		t.Fatalf("PublishRevision: %v", err)
	}
	if got := lessonIDs(published.LiveRevision.Sections[0]); !reflect.DeepEqual(got, desired) {
		t.Fatalf("live order after publication = %v, want %v", got, desired)
	}
	if published.LiveRevisionID == nil || *published.LiveRevisionID != candidate.ID {
		t.Fatalf("live revision = %v, want %s", published.LiveRevisionID, candidate.ID)
	}
}

func TestValidateExactOrder(t *testing.T) {
	want := []string{"a", "b", "c"}
	for name, tc := range map[string]struct {
		requested []string
		valid     bool
	}{
		"same": {[]string{"a", "b", "c"}, true}, "reverse": {[]string{"c", "b", "a"}, true},
		"duplicate": {[]string{"a", "a", "c"}, false}, "missing": {[]string{"a", "b"}, false},
		"foreign": {[]string{"a", "b", "d"}, false}, "empty exact": {[]string{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateExactOrder(want, tc.requested)
			if tc.valid && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidOrder) {
				t.Fatalf("error = %v, want ErrInvalidOrder", err)
			}
		})
	}
	if err := validateExactOrder(nil, nil); err != nil {
		t.Fatalf("empty authoritative order: %v", err)
	}
}
