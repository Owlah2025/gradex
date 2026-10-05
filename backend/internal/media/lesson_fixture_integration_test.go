//go:build integration

package media

import (
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

func seedMediaLesson(t *testing.T, f *mediaFixture, courseID string) string {
	t.Helper()
	var revision string
	err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_revisions WHERE course_id=$1::uuid AND state='DRAFT' LIMIT 1`, courseID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		revision = uuid.NewString()
		_, err = f.pool.Exec(f.ctx, `INSERT INTO course_revisions(id,course_id,state,revision_number,title_ar,title_en) VALUES($1::uuid,$2::uuid,'DRAFT',1,'اختبار','Test')`, revision, courseID)
	}
	if err != nil {
		t.Fatal(err)
	}
	section, lesson := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_section_identities(id,course_id) VALUES($1::uuid,$2::uuid)`, section, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_sections(id,revision_id,course_id,section_identity_id,title_ar,title_en,position) VALUES($1::uuid,$2::uuid,$3::uuid,$1::uuid,'اختبار','Test',(SELECT COALESCE(MAX(position),-1)+1 FROM course_sections WHERE revision_id=$2::uuid))`, section, revision, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lesson_identities(id,course_id,section_identity_id) VALUES($1::uuid,$2::uuid,$3::uuid)`, lesson, courseID, section); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lessons(id,section_id,course_id,section_identity_id,lesson_identity_id,title_ar,title_en,position) VALUES($1::uuid,$2::uuid,$3::uuid,$2::uuid,$1::uuid,'اختبار','Test',0)`, lesson, section, courseID); err != nil {
		t.Fatal(err)
	}
	return lesson
}
