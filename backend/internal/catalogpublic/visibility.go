package catalogpublic

import "fmt"

// VisibilityPredicate is the required policy every public catalogue read
// applies at its query boundary. It accepts SQL aliases so the one policy can
// be used consistently by list and detail consumers without duplicating a
// lifecycle condition.
type VisibilityPredicate func(courseAlias, revisionAlias string) string

// PublishedOnly is the complete public visibility rule. A public Course is
// published, not under emergency suspension, not retired, and represented by
// its current live revision.
func PublishedOnly(courseAlias, revisionAlias string) string {
	return fmt.Sprintf(
		"%s.lifecycle = 'PUBLISHED' AND %s.access_suspended_at IS NULL AND %s.retired_at IS NULL AND %s.live_revision_id = %s.id",
		courseAlias,
		courseAlias,
		courseAlias,
		courseAlias,
		revisionAlias,
	)
}
