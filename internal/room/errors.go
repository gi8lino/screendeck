package room

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound indicates that a requested room resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrForbidden indicates that the authenticated participant cannot perform an operation.
	ErrForbidden = errors.New("forbidden")
	// ErrMembershipConflict indicates that a browser identity is already linked to another participant.
	ErrMembershipConflict = errors.New("browser identity already linked to another room participant")
	// ErrLocked indicates that a room is not accepting new participants.
	ErrLocked = errors.New("room is locked")
	// ErrInvalidInput identifies a request that cannot be applied to a room.
	ErrInvalidInput = errors.New("invalid room input")
)

// InvalidInput returns a public room error with an actionable message.
func InvalidInput(message string) error {
	return invalidInputError{message: message}
}

// InvalidInputf returns a formatted public room error with an actionable message.
func InvalidInputf(format string, args ...any) error {
	return invalidInputError{message: fmt.Sprintf(format, args...)}
}

type invalidInputError struct {
	message string
}

func (e invalidInputError) Error() string { return e.message }

func (invalidInputError) Unwrap() error { return ErrInvalidInput }
