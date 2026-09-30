package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxAnnouncementTitleRunes = 140
	maxAnnouncementBodyRunes  = 4000
)

// Announcement is the public, immutable read model for a Course announcement.
// Account identity is deliberately not part of this response: the author is
// retained for provenance in the database, while Students only need the
// published message.
type Announcement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	PublishedAt time.Time `json:"published_at"`
}

type CreateAnnouncementRequest struct {
	CourseID        string
	AuthorAccountID string
	Title           string
	Body            string
	Now             time.Time
}

// AnnouncementReader is the narrow read seam consumed by protected learning.
type AnnouncementReader interface {
	ListPublishedCourseAnnouncements(context.Context, string) ([]Announcement, error)
}

func (r *Repository) CreateCourseAnnouncement(ctx context.Context, request CreateAnnouncementRequest) (Announcement, error) {
	if r == nil || r.pool == nil {
		return Announcement{}, ErrRepositoryNil
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Body = strings.TrimSpace(request.Body)
	if request.CourseID == "" || request.AuthorAccountID == "" || request.Now.IsZero() ||
		utf8.RuneCountInString(request.Title) == 0 || utf8.RuneCountInString(request.Title) > maxAnnouncementTitleRunes ||
		utf8.RuneCountInString(request.Body) == 0 || utf8.RuneCountInString(request.Body) > maxAnnouncementBodyRunes {
		return Announcement{}, ErrAnnouncementInvalid
	}

	var announcement Announcement
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		insertErr := tx.QueryRow(ctx, `
			INSERT INTO course_announcements (course_id, author_account_id, title, body, created_at, published_at)
			SELECT c.id, $2::uuid, $3, $4, $5::timestamptz, $5::timestamptz
			FROM courses c
			WHERE c.id = $1::uuid
			  AND c.owner_account_id = $2::uuid
			  AND c.lifecycle = 'PUBLISHED'
			RETURNING id::text, title, body, created_at, published_at
		`, request.CourseID, request.AuthorAccountID, request.Title, request.Body, request.Now.UTC()).Scan(
			&announcement.ID, &announcement.Title, &announcement.Body, &announcement.CreatedAt, &announcement.PublishedAt,
		)
		if errors.Is(insertErr, pgx.ErrNoRows) {
			var owner bool
			var lifecycle string
			checkErr := tx.QueryRow(ctx, `
				SELECT owner_account_id = $2::uuid, lifecycle::text
				FROM courses
				WHERE id = $1::uuid
			`, request.CourseID, request.AuthorAccountID).Scan(&owner, &lifecycle)
			if errors.Is(checkErr, pgx.ErrNoRows) || !owner {
				return ErrCourseNotFound
			}
			if checkErr != nil {
				return fmt.Errorf("checking course announcement ownership: %w", checkErr)
			}
			if lifecycle != string(LifecyclePublished) {
				return ErrCourseNotPublished
			}
			return fmt.Errorf("creating course announcement returned no row")
		}
		if insertErr != nil {
			return fmt.Errorf("creating course announcement: %w", insertErr)
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: request.AuthorAccountID, actorDescriptor: request.AuthorAccountID,
			action: "COURSE_ANNOUNCEMENT_PUBLISHED", targetType: "COURSE_ANNOUNCEMENT", targetID: announcement.ID,
			reason: "Course announcement published",
			metadata: map[string]any{
				"course_id":    request.CourseID,
				"title_length": utf8.RuneCountInString(request.Title),
				"body_length":  utf8.RuneCountInString(request.Body),
			},
		})
	})
	if err != nil {
		return Announcement{}, err
	}
	announcement.CreatedAt = announcement.CreatedAt.UTC()
	announcement.PublishedAt = announcement.PublishedAt.UTC()
	return announcement, nil
}

// ListOwnedCourseAnnouncements keeps both ownership and publication state in
// the SQL read. A non-owner gets ErrCourseNotFound, while an owner of a draft
// gets the explicit unpublished state required by the composer.
func (r *Repository) ListOwnedCourseAnnouncements(ctx context.Context, courseID, ownerAccountID string) ([]Announcement, error) {
	if r == nil || r.pool == nil {
		return nil, ErrRepositoryNil
	}
	if courseID == "" || ownerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	rows, err := r.pool.Query(ctx, `
		WITH authorized_course AS (
			SELECT id, lifecycle::text AS lifecycle
			FROM courses
			WHERE id = $1::uuid AND owner_account_id = $2::uuid
		)
		SELECT authorized_course.lifecycle,
		       announcement.id::text,
		       announcement.title,
		       announcement.body,
		       announcement.created_at,
		       announcement.published_at
		FROM authorized_course
		LEFT JOIN course_announcements announcement
		  ON announcement.course_id = authorized_course.id
		 AND authorized_course.lifecycle = 'PUBLISHED'
		ORDER BY announcement.published_at DESC NULLS LAST, announcement.id DESC
		LIMIT 100
	`, courseID, ownerAccountID)
	if err != nil {
		return nil, fmt.Errorf("listing owned course announcements: %w", err)
	}
	defer rows.Close()

	items := make([]Announcement, 0)
	seenCourse := false
	for rows.Next() {
		seenCourse = true
		var lifecycle string
		var itemID, title, body *string
		var createdAt, publishedAt *time.Time
		if err := rows.Scan(&lifecycle, &itemID, &title, &body, &createdAt, &publishedAt); err != nil {
			return nil, fmt.Errorf("scanning owned course announcement: %w", err)
		}
		if lifecycle != string(LifecyclePublished) {
			return nil, ErrCourseNotPublished
		}
		if itemID == nil || title == nil || body == nil || createdAt == nil || publishedAt == nil {
			continue
		}
		items = append(items, Announcement{
			ID: *itemID, Title: *title, Body: *body,
			CreatedAt: createdAt.UTC(), PublishedAt: publishedAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating owned course announcements: %w", err)
	}
	if !seenCourse {
		return nil, ErrCourseNotFound
	}
	return items, nil
}

func (r *Repository) ListPublishedCourseAnnouncements(ctx context.Context, courseID string) ([]Announcement, error) {
	if r == nil || r.pool == nil {
		return nil, ErrRepositoryNil
	}
	if courseID == "" {
		return nil, ErrCourseNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, title, body, created_at, published_at
		FROM course_announcements
		WHERE course_id = $1::uuid AND published_at IS NOT NULL
		ORDER BY published_at DESC, id DESC
		LIMIT 100
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("listing published course announcements: %w", err)
	}
	defer rows.Close()

	items := make([]Announcement, 0)
	for rows.Next() {
		var item Announcement
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.CreatedAt, &item.PublishedAt); err != nil {
			return nil, fmt.Errorf("scanning published course announcement: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.PublishedAt = item.PublishedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating published course announcements: %w", err)
	}
	return items, nil
}
