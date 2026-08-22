package httpapi

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// T072: no office-hours element renders on ST05 or ST06 (spec §Assumptions).
//
// `docs/SCREENS.md` lists "upcoming office hours" as content on the Student Dashboard and Course
// Home. S5 defers it to S17, and the assumption is specific about the shape of the deferral:
// absent, "not rendered as an empty or 'coming soon' state".
//
// The screens themselves are asserted in the frontend suite, over each screen's real render graph.
// What belongs here is the half a screen cannot control: an office-hours field arriving in the
// payload is the upstream of an element, and an empty array in the response is exactly how an empty
// state gets rendered. So the two read models that feed ST05 and ST06 are pinned to their properties.

// officeHoursConcepts is matched against text with every non-alphanumeric character removed, so
// `office_hours`, `officeHours`, `OfficeHours`, and `office hours` are one concept.
//
// Every entry is a compound. Bare `session`, `schedule`, and `meet` are deliberately absent: this
// package legitimately carries `authenticated_session`, `playback_session`, and `SessionID`, and a
// detector that fired on those would be switched off rather than fixed.
var officeHoursConcepts = []string{
	"officehour",
	"officesession",
	"upcomingsession",
	"livesession",
	"joinsession",
	"scheduledsession",
	"sessionschedule",
	"bookasession",
	"bookaslot",
	"webinar",
	"zoommeeting",
	"googlemeet",
	"meetinglink",
	"calendarinvite",
	"comingsoon",
}

var officeHoursSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeOfficeHours(source string) string {
	return officeHoursSeparators.ReplaceAllString(strings.ToLower(source), "")
}

// jsonPropertyNames walks a response DTO and returns every JSON property it can serialize,
// including nested objects and slice elements — an office-hours block would most likely arrive as a
// nested list rather than a top-level scalar.
func jsonPropertyNames(t *testing.T, value reflect.Type, seen map[reflect.Type]bool) []string {
	t.Helper()
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		value = value.Elem()
	}
	// time.Time is a struct but serializes as one RFC 3339 scalar; descending into it would report
	// its unexported clock internals as if they were payload properties.
	if value == reflect.TypeOf(time.Time{}) {
		return nil
	}
	if value.Kind() != reflect.Struct || seen[value] {
		return nil
	}
	seen[value] = true

	names := make([]string, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			names = append(names, name)
		}
		names = append(names, jsonPropertyNames(t, field.Type, seen)...)
	}
	return names
}

// TestDashboardAndCourseHomeReadModelsCarryNoOfficeHoursField is T072's payload half: the two
// screens' responses have no field an office-hours element could be rendered from.
func TestDashboardAndCourseHomeReadModelsCarryNoOfficeHoursField(t *testing.T) {
	models := map[string]any{
		"ST05 dashboard":   dashboardResponse{},
		"ST06 Course Home": courseHomeResponse{},
	}

	for screen, model := range models {
		properties := jsonPropertyNames(t, reflect.TypeOf(model), map[reflect.Type]bool{})
		if len(properties) == 0 {
			t.Fatalf("%s read model exposed no properties; the detector would pass vacuously", screen)
		}
		for _, property := range properties {
			normalized := normalizeOfficeHours(property)
			for _, concept := range officeHoursConcepts {
				if strings.Contains(normalized, concept) {
					t.Fatalf("%s read model carries the office-hours property %q; office hours are deferred to S17 "+
						"and appear on neither screen, not even as an empty state", screen, property)
				}
			}
		}
		t.Logf("%s read model properties: %v", screen, properties)
	}

	// The property sets are the accepted D-063 contracts, pinned so a new field must be justified
	// here rather than appearing silently.
	dashboardProperties := jsonPropertyNames(t, reflect.TypeOf(dashboardResponse{}), map[reflect.Type]bool{})
	if !containsAll(dashboardProperties, []string{"courses", "course_id", "title", "learning_status", "expires_at", "progress"}) {
		t.Fatalf("the ST05 read model no longer matches its accepted shape: %v", dashboardProperties)
	}
	courseHomeProperties := jsonPropertyNames(t, reflect.TypeOf(courseHomeResponse{}), map[reflect.Type]bool{})
	if !containsAll(courseHomeProperties, []string{"course_id", "title", "learning_status", "expires_at", "progress", "sections", "lessons", "resources", "lab_materials"}) {
		t.Fatalf("the ST06 read model no longer matches its accepted shape: %v", courseHomeProperties)
	}
}

func containsAll(haystack, needles []string) bool {
	present := make(map[string]bool, len(haystack))
	for _, value := range haystack {
		present[value] = true
	}
	for _, needle := range needles {
		if !present[needle] {
			return false
		}
	}
	return true
}

// TestRenderedDashboardAndCourseHomeJSONNamesNoOfficeHours checks the serialized bytes rather than
// the type, so a field added through an embedded struct or a custom marshaller is caught too.
func TestRenderedDashboardAndCourseHomeJSONNamesNoOfficeHours(t *testing.T) {
	populated := []any{
		dashboardResponse{Courses: []dashboardCourseResponse{{CourseID: "c", Title: "T"}}},
		courseHomeResponse{
			CourseID: "c", Title: "T",
			Sections: []courseHomeSectionResponse{{
				SectionID: "s", Title: "S",
				Lessons: []courseHomeLessonResponse{{LessonID: "l", Title: "L", Resources: []learningMaterialResponse{{Title: "Resource"}}}},
			}},
		},
	}

	for _, model := range populated {
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatalf("encoding read model: %v", err)
		}
		normalized := normalizeOfficeHours(string(encoded))
		for _, concept := range officeHoursConcepts {
			if strings.Contains(normalized, concept) {
				t.Fatalf("a rendered read model carries the office-hours concept %q: %s", concept, encoded)
			}
		}
	}
}

// TestOfficeHoursConceptScanIsUsableAndPrecise keeps the detector honest in both directions.
func TestOfficeHoursConceptScanIsUsableAndPrecise(t *testing.T) {
	for _, spelling := range []string{
		"office_hours", "officeHours", "OfficeHours", "office-hours", "office hours", "OFFICE_HOURS",
		"upcoming_sessions", "liveSession", "joinSession", "scheduled_session",
		"Book a slot", "webinarUrl", "zoomMeeting", "Google Meet", "meetingLink", "calendarInvite",
		"Coming soon", "Office hours — coming soon",
	} {
		normalized := normalizeOfficeHours(spelling)
		matched := false
		for _, concept := range officeHoursConcepts {
			if strings.Contains(normalized, concept) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("the concept scan missed %q (normalized %q)", spelling, normalized)
		}
	}

	// The vocabulary this package legitimately uses must never trip it.
	for _, benign := range []string{
		"authenticated_session", "playback_session", "sessionFromContext", "SessionID",
		"learning_status", "course_id", "expires_at", "report_context", "materials", "meetsThreshold",
	} {
		normalized := normalizeOfficeHours(benign)
		for _, concept := range officeHoursConcepts {
			if strings.Contains(normalized, concept) {
				t.Fatalf("the concept scan false-positived on %q via %q", benign, concept)
			}
		}
	}
}
