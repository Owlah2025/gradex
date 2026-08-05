package learning

import "testing"

func TestCourseGraphRejectsEmptyInputs(t *testing.T) {
	var repository *Repository
	if _, err := repository.ReadCourseGraph(t.Context(), "course"); err != ErrCourseGraphNotFound {
		t.Fatalf("nil repository graph error = %v, want ErrCourseGraphNotFound", err)
	}
	if _, err := repository.AggregateCourseProgress(t.Context(), "student", "course", CourseGraph{}); err != ErrEnrollmentNotFound {
		t.Fatalf("invalid aggregation error = %v, want ErrEnrollmentNotFound", err)
	}
}

func TestCourseGraphLessonIDsPreserveAuthoredOrder(t *testing.T) {
	graph := CourseGraph{
		Sections: []CourseGraphSection{
			{Lessons: []CourseGraphLesson{{ID: "lesson-1"}, {ID: "lesson-2"}}},
			{Lessons: []CourseGraphLesson{{ID: "lesson-3"}}},
		},
	}
	ids := graph.LessonIDs()
	if len(ids) != 3 || ids[0] != "lesson-1" || ids[1] != "lesson-2" || ids[2] != "lesson-3" {
		t.Fatalf("lesson IDs = %#v, want authored graph order", ids)
	}
}
