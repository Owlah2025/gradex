package auth

import "context"

// EntitlementChecker answers the two distinct "does this user have rights
// over this lesson" questions the video pipeline needs: student
// purchase/enrollment (playback) and instructor ownership (upload).
type EntitlementChecker interface {
	HasAccess(ctx context.Context, userID, lessonID string) (bool, error)
	IsInstructorForLesson(ctx context.Context, userID, lessonID string) (bool, error)
}
