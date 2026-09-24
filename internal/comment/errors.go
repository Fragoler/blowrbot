package comment

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is what a Repository returns for a missing row.
	ErrNotFound = errors.New("not found")

	ErrBadPayload          = errors.New("malformed deep link payload")
	ErrBanned              = errors.New("user is banned")
	ErrUnknownPost         = errors.New("unknown post")
	ErrNoNicknames         = errors.New("no active nicknames")
	ErrNoDraft             = errors.New("no active comment draft")
	ErrDraftExpired        = errors.New("comment draft expired")
	ErrNicknameUnavailable = errors.New("nickname is unavailable")
	ErrEmptyComment        = errors.New("comment has neither text nor media")
	ErrTextTooLong         = errors.New("comment text is too long")
)

// RejectedError is returned when a Guard blocks the comment. Reason is meant to be
// shown to the author, so it carries no internal details.
type RejectedError struct {
	Reason string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("comment rejected: %s", e.Reason)
}

// Rejected reports whether err came from a Guard and extracts it.
func Rejected(err error) (*RejectedError, bool) {
	var rejected *RejectedError
	ok := errors.As(err, &rejected)

	return rejected, ok
}
