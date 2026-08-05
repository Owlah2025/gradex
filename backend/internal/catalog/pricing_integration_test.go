//go:build integration

package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func setupPricingIntegrationTest(t *testing.T) (*Repository, string, string, string) {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)

	adminID := "10000000-0000-0000-0000-000000000001"
	instID := "10000000-0000-0000-0000-000000000002"
	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, role, status, email, normalized_email, display_name)
		VALUES
			($1::uuid, 'ADMIN', 'ACTIVE', 'admin@example.com', 'admin@example.com', 'Admin'),
			($2::uuid, 'INSTRUCTOR', 'ACTIVE', 'inst@example.com', 'inst@example.com', 'Instructor')
	`, adminID, instID)
	if err != nil {
		t.Fatalf("creating accounts: %v", err)
	}

	obWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("creating outbox writer: %v", err)
	}
	repo, err := NewRepository(p, obWriter)
	if err != nil {
		t.Fatalf("creating repository: %v", err)
	}

	course, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: instID,
		TitleAr:        "دورة اختبار التسعير",
		TitleEn:        "Pricing Test Course",
		DescriptionAr:  "وصف",
		DescriptionEn:  "Description",
	}, instID)
	if err != nil {
		t.Fatalf("creating course: %v", err)
	}

	return repo, adminID, instID, course.ID
}

func TestConcurrentPricingSerialization(t *testing.T) {
	repo, adminID, instID, courseID := setupPricingIntegrationTest(t)
	ctx := context.Background()

	const numWorkers = 10
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(val int64) {
			defer wg.Done()
			_, err := repo.SetCoursePrice(ctx, SetCoursePriceRequest{
				CourseID:        courseID,
				AdminAccountID:  adminID,
				ActorDescriptor: adminID,
				PriceMinorUnits: val * 1000,
				Reason:          fmt.Sprintf("Concurrent update %d", val),
			})
			if err != nil {
				errCh <- err
			}
		}(int64(i))
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent price update failed: %v", err)
	}

	history, err := repo.GetCoursePriceHistory(ctx, courseID)
	if err != nil {
		t.Fatalf("getting price history: %v", err)
	}
	if len(history) != numWorkers {
		t.Fatalf("got %d history records, want %d", len(history), numWorkers)
	}

	// Verify strict chronological order (history is in DESC order by changed_at)
	for i := 0; i < len(history)-1; i++ {
		if !history[i].ChangedAt.After(history[i+1].ChangedAt) {
			t.Errorf("history[%d].ChangedAt (%v) is not strictly after history[%d].ChangedAt (%v)",
				i, history[i].ChangedAt, i+1, history[i+1].ChangedAt)
		}
	}

	// Verify unbroken old -> new value chain
	chronoHistory := make([]PriceChange, len(history))
	for i := range history {
		chronoHistory[len(history)-1-i] = history[i]
	}

	if chronoHistory[0].OldValueMinorUnits != nil {
		t.Errorf("initial price change old value = %v, want nil", chronoHistory[0].OldValueMinorUnits)
	}

	for i := 1; i < len(chronoHistory); i++ {
		prevNew := chronoHistory[i-1].NewValueMinorUnits
		currOld := chronoHistory[i].OldValueMinorUnits
		if currOld == nil || *currOld != prevNew {
			t.Errorf("price change %d old_value = %v, want previous new_value %d", i, currOld, prevNew)
		}
	}

	// Verify final derived current price matches newest history row
	c, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("getting owned course: %v", err)
	}
	if c.PriceMinorUnits == nil || *c.PriceMinorUnits != history[0].NewValueMinorUnits {
		t.Errorf("derived course price = %v, want newest history price %d", c.PriceMinorUnits, history[0].NewValueMinorUnits)
	}
}

func TestCrossCourseSectionRefusal(t *testing.T) {
	repo, adminID, instID, course1ID := setupPricingIntegrationTest(t)
	ctx := context.Background()

	course1, err := repo.GetOwnedCourse(ctx, course1ID, instID)
	if err != nil {
		t.Fatalf("getting owned course 1: %v", err)
	}
	cand1 := course1.EditableRevision

	sec1, err := repo.AddSection(ctx, AddSectionRequest{
		CourseID:       course1ID,
		RevisionID:     cand1.ID,
		OwnerAccountID: instID,
		TitleAr:        "قسم 1",
		TitleEn:        "Section 1",
	}, instID)
	if err != nil {
		t.Fatalf("adding section 1: %v", err)
	}

	course2, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: instID,
		TitleAr:        "دورة ثانية",
		TitleEn:        "Second Course",
		DescriptionAr:  "وصف",
		DescriptionEn:  "Description",
	}, instID)
	if err != nil {
		t.Fatalf("creating course 2: %v", err)
	}

	_, err = repo.SetSectionPrice(ctx, SetSectionPriceRequest{
		CourseID:          course2.ID,
		SectionIdentityID: sec1.SectionIdentityID,
		AdminAccountID:    adminID,
		ActorDescriptor:   adminID,
		PriceMinorUnits:   500,
		Reason:            "Cross course attempt",
	})
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("cross-course section price update err = %v, want ErrCourseNotFound", err)
	}
}

func TestExistingCommerceAndAccessFixturesUnchanged(t *testing.T) {
	repo, adminID, instID, courseID := setupPricingIntegrationTest(t)
	ctx := context.Background()
	p := repo.Pool()

	// fake_entitlements remains a legacy S1 fixture; S5 Progress is no longer
	// keyed to this legacy Lesson graph.
	studentID := "20000000-0000-0000-0000-000000000001"
	legacyCourseID := "30000000-0000-0000-0000-000000000001"
	legacySecID := "30000000-0000-0000-0000-000000000002"
	legacyLesID := "30000000-0000-0000-0000-000000000003"

	if _, err := p.Exec(ctx, `
		INSERT INTO accounts (id, role, status, email, normalized_email, display_name)
		VALUES ($1::uuid, 'STUDENT', 'ACTIVE', 'student@example.com', 'student@example.com', 'Student')
	`, studentID); err != nil {
		t.Fatalf("seeding student account: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')
	`, legacyCourseID, instID); err != nil {
		t.Fatalf("seeding legacy course: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO sections (id, course_id, title, "order") VALUES ($1::uuid, $2::uuid, 'Sec', 1)
	`, legacySecID, legacyCourseID); err != nil {
		t.Fatalf("seeding legacy section: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO lessons (id, section_id, title, "order") VALUES ($1::uuid, $2::uuid, 'Les', 1)
	`, legacyLesID, legacySecID); err != nil {
		t.Fatalf("seeding legacy lesson: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO fake_entitlements (user_id, lesson_id, role)
		VALUES ($1::uuid, $2::uuid, 'student')
	`, studentID, legacyLesID); err != nil {
		t.Fatalf("seeding fake_entitlements fixture: %v", err)
	}

	type entitlementRecord struct {
		UserID   string
		LessonID string
		Role     string
	}

	fetchEntitlement := func() entitlementRecord {
		var er entitlementRecord
		err := p.QueryRow(ctx, `
			SELECT user_id, lesson_id, role
			FROM fake_entitlements WHERE user_id = $1::uuid AND lesson_id = $2::uuid
		`, studentID, legacyLesID).Scan(&er.UserID, &er.LessonID, &er.Role)
		if err != nil {
			t.Fatalf("querying fake_entitlements fixture: %v", err)
		}

		return er
	}

	preEntitlement := fetchEntitlement()

	// Add section to courseID for testing section price change
	c, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("getting owned course: %v", err)
	}
	sec, err := repo.AddSection(ctx, AddSectionRequest{
		CourseID:       courseID,
		RevisionID:     c.EditableRevision.ID,
		OwnerAccountID: instID,
		TitleAr:        "قسم التست",
		TitleEn:        "Test Section",
	}, instID)
	if err != nil {
		t.Fatalf("adding section: %v", err)
	}

	// Perform Course and Section price changes
	_, err = repo.SetCoursePrice(ctx, SetCoursePriceRequest{
		CourseID:        courseID,
		AdminAccountID:  adminID,
		ActorDescriptor: adminID,
		PriceMinorUnits: 25000,
		Reason:          "Course launch price",
	})
	if err != nil {
		t.Fatalf("setting course price: %v", err)
	}

	_, err = repo.SetSectionPrice(ctx, SetSectionPriceRequest{
		CourseID:          courseID,
		SectionIdentityID: sec.SectionIdentityID,
		AdminAccountID:    adminID,
		ActorDescriptor:   adminID,
		PriceMinorUnits:   10000,
		Reason:            "Section standalone price",
	})
	if err != nil {
		t.Fatalf("setting section price: %v", err)
	}

	postEntitlement := fetchEntitlement()
	if !reflect.DeepEqual(preEntitlement, postEntitlement) {
		t.Errorf("fake_entitlements fixture changed!\nBefore: %+v\nAfter:  %+v", preEntitlement, postEntitlement)
	}
}

func TestOwnedReadContract_UnpricedPricedAndSectionPriced(t *testing.T) {
	repo, adminID, instID, courseID := setupPricingIntegrationTest(t)
	ctx := context.Background()

	// 1. Unpriced state: both ListOwnedCourses and GetOwnedCourse return nil price
	listUnpriced, err := repo.ListOwnedCourses(ctx, instID)
	if err != nil {
		t.Fatalf("ListOwnedCourses unpriced err: %v", err)
	}
	if len(listUnpriced) == 0 {
		t.Fatalf("ListOwnedCourses unpriced returned 0 courses, want at least 1")
	}

	var foundUnpriced *Course
	for i := range listUnpriced {
		if listUnpriced[i].ID == courseID {
			foundUnpriced = &listUnpriced[i]
			break
		}
	}
	if foundUnpriced == nil {
		t.Fatalf("ListOwnedCourses unpriced did not contain courseID %s", courseID)
	}
	if foundUnpriced.PriceMinorUnits != nil {
		t.Errorf("ListOwnedCourses unpriced got PriceMinorUnits = %v, want nil", foundUnpriced.PriceMinorUnits)
	}

	detailUnpriced, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("GetOwnedCourse unpriced err: %v", err)
	}
	if detailUnpriced.PriceMinorUnits != nil {
		t.Errorf("GetOwnedCourse unpriced got PriceMinorUnits = %v, want nil", detailUnpriced.PriceMinorUnits)
	}

	// 2. Add a section to the course revision
	sec, err := repo.AddSection(ctx, AddSectionRequest{
		CourseID:       courseID,
		RevisionID:     detailUnpriced.EditableRevision.ID,
		OwnerAccountID: instID,
		TitleAr:        "قسم 1",
		TitleEn:        "Section 1",
	}, instID)
	if err != nil {
		t.Fatalf("AddSection err: %v", err)
	}

	// Verify section is unpriced initially
	detailSecUnpriced, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("GetOwnedCourse after section add err: %v", err)
	}
	if len(detailSecUnpriced.EditableRevision.Sections) != 1 {
		t.Fatalf("sections count = %d, want 1", len(detailSecUnpriced.EditableRevision.Sections))
	}
	if detailSecUnpriced.EditableRevision.Sections[0].PriceMinorUnits != nil {
		t.Errorf("section unpriced got PriceMinorUnits = %v, want nil", detailSecUnpriced.EditableRevision.Sections[0].PriceMinorUnits)
	}

	// 3. Set Course Price (25,000 fils) -> ListOwnedCourses and GetOwnedCourse return 25,000 fils
	_, err = repo.SetCoursePrice(ctx, SetCoursePriceRequest{
		CourseID:        courseID,
		AdminAccountID:  adminID,
		ActorDescriptor: adminID,
		PriceMinorUnits: 25000,
		Reason:          "Set course price to 25.000 KWD",
	})
	if err != nil {
		t.Fatalf("SetCoursePrice err: %v", err)
	}

	listPriced, err := repo.ListOwnedCourses(ctx, instID)
	if err != nil {
		t.Fatalf("ListOwnedCourses priced err: %v", err)
	}
	if len(listPriced) == 0 {
		t.Fatalf("ListOwnedCourses priced returned 0 courses, want at least 1")
	}

	var foundPriced *Course
	for i := range listPriced {
		if listPriced[i].ID == courseID {
			foundPriced = &listPriced[i]
			break
		}
	}
	if foundPriced == nil {
		t.Fatalf("ListOwnedCourses priced did not contain courseID %s", courseID)
	}
	if foundPriced.PriceMinorUnits == nil || *foundPriced.PriceMinorUnits != 25000 {
		t.Errorf("ListOwnedCourses priced got PriceMinorUnits = %v, want 25000", foundPriced.PriceMinorUnits)
	}

	detailPriced, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("GetOwnedCourse priced err: %v", err)
	}
	if detailPriced.PriceMinorUnits == nil || *detailPriced.PriceMinorUnits != 25000 {
		t.Errorf("GetOwnedCourse priced got PriceMinorUnits = %v, want 25000", detailPriced.PriceMinorUnits)
	}

	// 4. Set Section Price (10,000 fils) -> exact Section in revision graph returns 10,000 fils
	_, err = repo.SetSectionPrice(ctx, SetSectionPriceRequest{
		CourseID:          courseID,
		SectionIdentityID: sec.SectionIdentityID,
		AdminAccountID:    adminID,
		ActorDescriptor:   adminID,
		PriceMinorUnits:   10000,
		Reason:            "Set section price to 10.000 KWD",
	})
	if err != nil {
		t.Fatalf("SetSectionPrice err: %v", err)
	}

	detailSecPriced, err := repo.GetOwnedCourse(ctx, courseID, instID)
	if err != nil {
		t.Fatalf("GetOwnedCourse after section price err: %v", err)
	}
	if len(detailSecPriced.EditableRevision.Sections) != 1 {
		t.Fatalf("sections count = %d, want 1", len(detailSecPriced.EditableRevision.Sections))
	}
	secRead := detailSecPriced.EditableRevision.Sections[0]
	if secRead.SectionIdentityID != sec.SectionIdentityID {
		t.Errorf("section identity ID = %s, want %s", secRead.SectionIdentityID, sec.SectionIdentityID)
	}
	if secRead.PriceMinorUnits == nil || *secRead.PriceMinorUnits != 10000 {
		t.Errorf("section PriceMinorUnits = %v, want 10000", secRead.PriceMinorUnits)
	}
}
