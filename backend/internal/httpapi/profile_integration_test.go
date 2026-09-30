//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestT4InstructorProfilesAndStudentProfile(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()
	const (
		institutionID = "30000000-0000-0000-0000-000000000001"
		subjectID     = "30000000-0000-0000-0000-000000000002"
		instructorID  = "20000000-0000-0000-0000-000000000002"
		instructorBID = "20000000-0000-0000-0000-000000000004"
	)
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ($1::uuid, 'KW', 'profile-university', 'جامعة الملف', 'Profile University')
	`, institutionID); err != nil {
		t.Fatalf("seeding profile institution: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, 'PR-101', 'مادة الملف', 'Profile Subject')
	`, subjectID, institutionID); err != nil {
		t.Fatalf("seeding profile subject: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		('20000000-0000-0000-0000-000000000005', 'queue-one@example.com', 'queue-one@example.com', 'INSTRUCTOR', 'ACTIVE', 'Queue One'),
		('20000000-0000-0000-0000-000000000006', 'queue-two@example.com', 'queue-two@example.com', 'INSTRUCTOR', 'ACTIVE', 'Queue Two');
		INSERT INTO instructor_profiles (account_id, public_slug) VALUES
		('20000000-0000-0000-0000-000000000005', 'queue-one'),
		('20000000-0000-0000-0000-000000000006', 'queue-two')
	`); err != nil {
		t.Fatalf("seeding instructor queue profiles: %v", err)
	}

	t.Run("admin queue returns consistent pagination metadata", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/admin/instructor-profiles?state=DRAFT&page=1&limit=1", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("profile queue page 1 status = %d; body %s", status, raw)
		}
		var page struct {
			Items   []map[string]any `json:"items"`
			Page    int              `json:"page"`
			Limit   int              `json:"limit"`
			HasMore bool             `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decoding profile queue page 1: %v", err)
		}
		if len(page.Items) != 1 || page.Page != 1 || page.Limit != 1 || !page.HasMore {
			t.Fatalf("profile queue page 1 = %#v, want one item with has_more", page)
		}
		status, raw = env.call(t, http.MethodGet, "/api/v1/admin/instructor-profiles?state=DRAFT&page=2&limit=1", env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("profile queue page 2 status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decoding profile queue page 2: %v", err)
		}
		if len(page.Items) != 1 || page.Page != 2 || page.Limit != 1 {
			t.Fatalf("profile queue page 2 = %#v, want one item on page 2", page)
		}
	})

	t.Run("student and anonymous cannot use instructor routes", func(t *testing.T) {
		status, _ := env.call(t, http.MethodGet, "/api/v1/me/instructor-profile", env.studentToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("student instructor profile status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/me/profile", "", nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous profile status = %d, want 401", status)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/me/profile", env.instructorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("instructor student profile status = %d, want 403", status)
		}
		for _, test := range []struct {
			name  string
			token string
		}{
			{name: "student", token: env.studentToken},
			{name: "instructor", token: env.instructorToken},
		} {
			t.Run(test.name, func(t *testing.T) {
				status, _ := env.call(t, http.MethodGet, "/api/v1/admin/instructor-profiles", test.token, nil)
				if status != http.StatusForbidden {
					t.Fatalf("admin profile list status = %d, want 403", status)
				}
				status, _ = env.call(t, http.MethodGet, "/api/v1/admin/instructor-profiles/"+instructorBID, test.token, nil)
				if status != http.StatusForbidden {
					t.Fatalf("admin profile detail status = %d, want 403", status)
				}
				status, _ = env.call(t, http.MethodPost, "/api/v1/admin/instructor-profiles/"+instructorBID+"/approve", test.token, map[string]any{
					"revision": 1, "reason": "unauthorized",
				})
				if status != http.StatusForbidden {
					t.Fatalf("admin profile decision status = %d, want 403", status)
				}
			})
		}
	})

	t.Run("student my profile is own data and edits audit", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/me/profile", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("student profile GET status = %d; body %s", status, raw)
		}
		var profile map[string]any
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding student profile: %v", err)
		}
		if profile["email"] != "cat-student@example.com" || profile["display_name"] != "Catalog Student" {
			t.Fatalf("student profile returned wrong identity: %v", profile)
		}
		status, raw = env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{
			"display_name": "Updated Student", "locale": "en",
		})
		if status != http.StatusOK {
			t.Fatalf("student profile PUT status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding updated student profile: %v", err)
		}
		if profile["display_name"] != "Updated Student" || profile["locale"] != "en" {
			t.Fatalf("student profile update = %v", profile)
		}
		status, raw = env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{
			"display_name": "  Updated Student  ", "locale": "en",
		})
		if status != http.StatusOK {
			t.Fatalf("trimmed student profile PUT status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding trimmed student profile: %v", err)
		}
		if profile["display_name"] != "Updated Student" {
			t.Fatalf("trimmed display name = %v", profile["display_name"])
		}
		var auditCount int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_events
			WHERE action = 'ACCOUNT_PROFILE_UPDATED' AND target_id = $1
		`, "20000000-0000-0000-0000-000000000003").Scan(&auditCount); err != nil {
			t.Fatalf("counting profile audit: %v", err)
		}
		if auditCount != 2 {
			t.Fatalf("account profile audit rows = %d, want 2", auditCount)
		}
		var changedFields []string
		if err := env.pool.QueryRow(ctx, `
			SELECT ARRAY(
				SELECT jsonb_array_elements_text(metadata->'changed_fields')
			)
			FROM audit_events
			WHERE action = 'ACCOUNT_PROFILE_UPDATED' AND target_id = $1
			ORDER BY occurred_at DESC, id DESC
			LIMIT 1
		`, "20000000-0000-0000-0000-000000000003").Scan(&changedFields); err != nil {
			t.Fatalf("reading unchanged profile audit fields: %v", err)
		}
		if len(changedFields) != 0 {
			t.Fatalf("unchanged account profile audit fields = %v, want empty", changedFields)
		}
	})

	t.Run("instructor moderation and public snapshot", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/me/instructor-profile", env.instructorToken, nil)
		if status != http.StatusOK {
			t.Fatalf("initial instructor profile status = %d; body %s", status, raw)
		}
		var profile struct {
			Revision int `json:"revision"`
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding initial instructor profile: %v", err)
		}
		if profile.Revision != 0 {
			t.Fatalf("initial instructor profile revision = %d, want 0", profile.Revision)
		}

		status, raw = env.call(t, http.MethodPut, "/api/v1/me/instructor-profile", env.instructorToken, map[string]any{
			"revision": 0, "public_slug": "prof-instructor",
			"headline_ar": "", "headline_en": "Computer science, made clear",
			"bio_ar": "", "bio_en": "I teach the ideas behind the systems students build.",
			"expertise_ids": []string{subjectID},
		})
		if status != http.StatusOK {
			t.Fatalf("instructor draft PUT status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding instructor draft: %v", err)
		}
		if profile.Revision != 1 {
			t.Fatalf("draft revision = %d, want 1", profile.Revision)
		}
		status, raw = env.call(t, http.MethodPost, "/api/v1/me/instructor-profile/submission", env.instructorToken, map[string]any{"revision": 1})
		if status != http.StatusOK {
			t.Fatalf("instructor submission status = %d; body %s", status, raw)
		}
		status, _ = env.call(t, http.MethodPut, "/api/v1/me/instructor-profile", env.instructorToken, map[string]any{
			"revision": 2, "public_slug": "unreviewed-edit",
			"headline_ar": "", "headline_en": "Unreviewed edit",
			"bio_ar": "", "bio_en": "This must not be saved while under review.",
			"expertise_ids": []string{subjectID},
		})
		if status != http.StatusConflict {
			t.Fatalf("edit during review status = %d, want 409", status)
		}
		status, _ = env.call(t, http.MethodPost,
			"/api/v1/admin/instructor-profiles/"+instructorID+"/approve",
			env.adminToken, map[string]any{"revision": 1, "reason": "stale review"})
		if status != http.StatusConflict {
			t.Fatalf("stale approval status = %d, want 409", status)
		}

		status, raw = env.call(t, http.MethodPost,
			"/api/v1/admin/instructor-profiles/"+instructorID+"/request-changes",
			env.adminToken, map[string]any{"revision": 2, "reason": "Add a concrete example to the biography."})
		if status != http.StatusOK {
			t.Fatalf("request changes status = %d; body %s", status, raw)
		}
		status, raw = env.call(t, http.MethodPost, "/api/v1/me/instructor-profile/submission", env.instructorToken, map[string]any{"revision": 3})
		if status != http.StatusOK {
			t.Fatalf("resubmission status = %d; body %s", status, raw)
		}
		status, raw = env.call(t, http.MethodPost,
			"/api/v1/admin/instructor-profiles/"+instructorID+"/approve",
			env.adminToken, map[string]any{"revision": 4, "reason": "Profile is complete and ready for students."})
		if status != http.StatusOK {
			t.Fatalf("approval status = %d; body %s", status, raw)
		}
		status, raw = env.call(t, http.MethodGet, "/api/v1/me/instructor-profile", env.instructorToken, nil)
		if status != http.StatusOK {
			t.Fatalf("instructor profile after approval status = %d; body %s", status, raw)
		}
		var ownProfile map[string]any
		if err := json.Unmarshal(raw, &ownProfile); err != nil {
			t.Fatalf("decoding instructor profile after approval: %v", err)
		}
		if _, exposed := ownProfile["decided_by"]; exposed {
			t.Fatalf("instructor response exposes decided_by: %v", ownProfile)
		}

		var courseID, revisionID string
		if err := env.pool.QueryRow(ctx, `
			INSERT INTO courses (owner_account_id, lifecycle)
			VALUES ($1::uuid, 'DRAFT') RETURNING id::text
		`, instructorID).Scan(&courseID); err != nil {
			t.Fatalf("seeding instructor course: %v", err)
		}
		if err := env.pool.QueryRow(ctx, `
			INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
			VALUES ($1::uuid, 'APPROVED', 1, 'مقرر عام', 'Public instructor course')
			RETURNING id::text
		`, courseID).Scan(&revisionID); err != nil {
			t.Fatalf("seeding instructor course revision: %v", err)
		}
		if _, err := env.pool.Exec(ctx, `
			UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid
			WHERE id = $2::uuid
		`, revisionID, courseID); err != nil {
			t.Fatalf("publishing instructor course: %v", err)
		}

		status, raw = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/prof-instructor", "", nil)
		if status != http.StatusOK {
			t.Fatalf("public instructor status = %d; body %s", status, raw)
		}
		var public map[string]any
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatalf("decoding public instructor: %v", err)
		}
		if public["headline_en"] != "Computer science, made clear" {
			t.Fatalf("public headline = %v", public["headline_en"])
		}
		courses, ok := public["courses"].([]any)
		if !ok || len(courses) != 1 {
			t.Fatalf("public instructor courses = %v", public["courses"])
		}
		if courses[0].(map[string]any)["instructor_slug"] != "prof-instructor" {
			t.Fatalf("course instructor slug = %v", courses[0].(map[string]any)["instructor_slug"])
		}

		status, raw = env.call(t, http.MethodPut, "/api/v1/me/instructor-profile", env.instructorToken, map[string]any{
			"revision": 5, "public_slug": "draft-only-slug",
			"headline_ar": "", "headline_en": "Draft-only headline",
			"bio_ar": "", "bio_en": "A newer private draft.",
			"expertise_ids": []string{subjectID},
		})
		if status != http.StatusOK {
			t.Fatalf("published profile draft edit status = %d; body %s", status, raw)
		}
		status, raw = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/prof-instructor", "", nil)
		if status != http.StatusOK {
			t.Fatalf("public instructor after draft edit status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatalf("decoding public instructor after draft edit: %v", err)
		}
		if public["headline_en"] != "Computer science, made clear" {
			t.Fatalf("draft leaked into public snapshot: %v", public["headline_en"])
		}
		courses, ok = public["courses"].([]any)
		if !ok || len(courses) != 1 || courses[0].(map[string]any)["instructor_slug"] != "prof-instructor" {
			t.Fatalf("draft slug leaked into course projection: %v", public["courses"])
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/draft-only-slug", "", nil)
		if status != http.StatusNotFound {
			t.Fatalf("draft instructor slug public status = %d, want 404", status)
		}

		status, _ = env.call(t, http.MethodPost, "/api/v1/me/instructor-profile/submission", env.instructorToken, map[string]any{"revision": 6})
		if status != http.StatusOK {
			t.Fatalf("published profile resubmission status = %d, want 200", status)
		}
		status, _ = env.call(t, http.MethodPost,
			"/api/v1/admin/instructor-profiles/"+instructorID+"/request-changes",
			env.adminToken, map[string]any{"revision": 7, "reason": "Please refine the new draft."})
		if status != http.StatusOK {
			t.Fatalf("published profile request changes status = %d, want 200", status)
		}
		status, raw = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/prof-instructor", "", nil)
		if status != http.StatusOK {
			t.Fatalf("public instructor after resubmission status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatalf("decoding public instructor after resubmission: %v", err)
		}
		if public["headline_en"] != "Computer science, made clear" {
			t.Fatalf("approved snapshot disappeared during review: %v", public["headline_en"])
		}
		courses, ok = public["courses"].([]any)
		if !ok || len(courses) != 1 || courses[0].(map[string]any)["instructor_slug"] != "prof-instructor" {
			t.Fatalf("course projection disappeared during review: %v", public["courses"])
		}

		status, _ = env.call(t, http.MethodPut, "/api/v1/me/instructor-profile", env.instructorBToken, map[string]any{
			"revision": 0, "public_slug": "prof-instructor",
			"headline_ar": "", "headline_en": "Second instructor",
			"bio_ar": "", "bio_en": "A second biography.",
			"expertise_ids": []string{subjectID},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("published slug collision status = %d, want 422", status)
		}
		status, _ = env.call(t, http.MethodPut, "/api/v1/me/instructor-profile", env.instructorBToken, map[string]any{
			"revision": 0, "public_slug": "Not A Valid Slug",
			"headline_ar": "", "headline_en": "Second instructor",
			"bio_ar": "", "bio_en": "A second biography.",
			"expertise_ids": []string{subjectID},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid slug status = %d, want 422", status)
		}

		status, raw = env.call(t, http.MethodPost,
			"/api/v1/admin/instructor-profiles/"+instructorID+"/hide",
			env.adminToken, map[string]any{"revision": 8, "reason": "Temporarily hidden for a content review."})
		if status != http.StatusOK {
			t.Fatalf("hide status = %d; body %s", status, raw)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/prof-instructor", "", nil)
		if status != http.StatusNotFound {
			t.Fatalf("hidden instructor public status = %d, want 404", status)
		}
		status, _ = env.call(t, http.MethodPost, "/api/v1/me/instructor-profile/submission", env.instructorToken, map[string]any{"revision": 9})
		if status != http.StatusOK {
			t.Fatalf("hidden profile resubmission status = %d, want 200", status)
		}
		for _, slug := range []string{"prof-instructor", "draft-only-slug"} {
			status, _ = env.call(t, http.MethodGet, "/api/v1/catalog/instructors/"+slug, "", nil)
			if status != http.StatusNotFound {
				t.Fatalf("hidden resubmitted slug %s status = %d, want 404", slug, status)
			}
		}

		var moderationAudits int
		if err := env.pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_events
			WHERE target_type = 'INSTRUCTOR_PROFILE' AND target_id = $1
			  AND action IN (
				'INSTRUCTOR_PROFILE_CHANGES_REQUESTED',
				'INSTRUCTOR_PROFILE_APPROVED',
				'INSTRUCTOR_PROFILE_HIDDEN'
			  )
		`, instructorID).Scan(&moderationAudits); err != nil {
			t.Fatalf("counting instructor moderation audits: %v", err)
		}
		if moderationAudits != 4 {
			t.Fatalf("instructor moderation audit rows = %d, want 4", moderationAudits)
		}
	})
}
