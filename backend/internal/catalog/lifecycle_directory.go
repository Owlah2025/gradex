package catalog

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CourseLifecycleSummary is one row of the Admin lifecycle directory: the state a lifecycle
// decision is taken against, and enough human-readable identity to take it without handling a
// UUID. It deliberately carries no revision graph, no price and no academic projection — a
// lifecycle command reads none of those.
type CourseLifecycleSummary struct {
	ID                     string          `json:"id"`
	TitleAr                string          `json:"title_ar"`
	TitleEn                string          `json:"title_en"`
	OwnerDisplayName       string          `json:"owner_display_name"`
	Lifecycle              CourseLifecycle `json:"lifecycle"`
	AccessSuspendedAt      *time.Time      `json:"access_suspended_at,omitempty"`
	AccessSuspensionReason *string         `json:"access_suspension_reason,omitempty"`
	RetiredAt              *time.Time      `json:"retired_at,omitempty"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// LifecycleDirectoryLimit bounds one Admin directory read. The directory is a working surface for
// a lifecycle decision, not a catalogue export.
const LifecycleDirectoryLimit = 50

// ListCourseLifecycleDirectory returns Courses in every lifecycle state, which is exactly what the
// public catalogue must never do: a delisted, retired or archived Course is invisible publicly and
// still has to be reachable by the Admin who has to relist it or read its current state.
//
// The title comes from the live revision when there is one and from the newest revision otherwise,
// so a Course is identified by the words a human knows it by in every state.
func (r *Repository) ListCourseLifecycleDirectory(ctx context.Context, search string) ([]CourseLifecycleSummary, error) {
	normalized := strings.TrimSpace(search)

	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, COALESCE(cr.title_ar, ''), COALESCE(cr.title_en, ''),
		       COALESCE(a.display_name, ''), c.lifecycle::text,
		       c.access_suspended_at, c.access_suspension_reason, c.retired_at, c.updated_at
		FROM courses c
		JOIN accounts a ON a.id = c.owner_account_id
		LEFT JOIN LATERAL (
			SELECT title_ar, title_en
			FROM course_revisions
			WHERE course_id = c.id
			ORDER BY (id = c.live_revision_id) DESC, revision_number DESC
			LIMIT 1
		) cr ON TRUE
		WHERE $1 = ''
		   OR COALESCE(cr.title_en, '') ILIKE '%' || $1 || '%'
		   OR COALESCE(cr.title_ar, '') ILIKE '%' || $1 || '%'
		ORDER BY c.updated_at DESC, c.id
		LIMIT $2
	`, normalized, LifecycleDirectoryLimit)
	if err != nil {
		return nil, fmt.Errorf("listing course lifecycle directory: %w", err)
	}
	defer rows.Close()

	summaries := make([]CourseLifecycleSummary, 0)
	for rows.Next() {
		var summary CourseLifecycleSummary
		if err := rows.Scan(
			&summary.ID, &summary.TitleAr, &summary.TitleEn, &summary.OwnerDisplayName,
			&summary.Lifecycle, &summary.AccessSuspendedAt, &summary.AccessSuspensionReason,
			&summary.RetiredAt, &summary.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning course lifecycle summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading course lifecycle directory: %w", err)
	}
	return summaries, nil
}
