package access

import "errors"

var (
	ErrInvitationNotFound      = errors.New("invitation not found")
	ErrInvitationStateConflict = errors.New("invitation state conflict")
	ErrDuplicateInvitation     = errors.New("duplicate invitation")
)
