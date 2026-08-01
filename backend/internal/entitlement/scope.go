package entitlement

// Covers evaluates an already-authoritative Course graph node. A Course grant
// reaches every Section in that Course; a Section grant reaches only the exact
// stable Section identity. No grant can cross Courses.
func Covers(record Record, lesson Lesson) bool {
	if record.CourseID == "" || lesson.CourseID == "" || record.CourseID != lesson.CourseID {
		return false
	}
	switch record.ScopeKind {
	case ScopeCourse:
		return record.ScopeID == lesson.CourseID
	case ScopeSection:
		return record.ScopeID != "" && record.ScopeID == lesson.SectionID
	default:
		return false
	}
}
