package room

import "errors"

var (
	// ErrNotFound indicates that a requested room resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrForbidden indicates that the authenticated participant cannot perform an operation.
	ErrForbidden = errors.New("forbidden")
	// ErrMembershipConflict indicates that a browser identity is already linked to another participant.
	ErrMembershipConflict = errors.New("browser identity already linked to another room participant")
	// ErrLocked indicates that a room is not accepting new participants.
	ErrLocked = errors.New("room is locked")
)
