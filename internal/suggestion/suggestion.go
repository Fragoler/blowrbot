// Package suggestion records posts offered through the channel's Direct Messages
// and the decisions admins make on them.
//
// The bot does not moderate: approving and declining happen in Telegram's own
// Direct Messages interface, and the bot learns the outcome from the service
// message that follows. That keeps one source of truth — a decision taken in the
// app can never disagree with what the database thinks.
package suggestion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"loudbot/internal/comment"
)

// Status is where a suggestion stands.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDeclined Status = "declined"
	// StatusFailed is Telegram refusing an approval, for instance when the
	// channel lost the right to publish it.
	StatusFailed Status = "failed"
)

// ErrNotFound is what a Repository returns for a suggestion it does not hold.
var ErrNotFound = errors.New("suggestion not found")

// Suggestion is one post offered by a reader.
type Suggestion struct {
	ID     int64
	UserID int64
	Text   string
	Media  []comment.Media
	Status Status
	// DMChatID and DMMessageID address the original message in the channel's
	// Direct Messages chat. A decision arrives carrying that message, not our id,
	// so this pair is how the two are matched up.
	DMChatID    int64
	DMMessageID int
	CreatedAt   time.Time
}

type Repository interface {
	EnsureUser(ctx context.Context, userID int64) (comment.User, error)
	CreateSuggestion(ctx context.Context, s Suggestion) (int64, error)
	SuggestionByMessage(ctx context.Context, chatID int64, messageID int) (Suggestion, error)
	SetSuggestionStatus(ctx context.Context, id int64, status Status, decidedAt time.Time) error
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func New(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// SetClock replaces the time source; used by tests.
func (s *Service) SetClock(now func() time.Time) {
	s.now = now
}

// Offered records a new suggestion. Telegram redelivers updates, so an offer
// already on file is left alone rather than duplicated.
func (s *Service) Offered(ctx context.Context, sug Suggestion) (Suggestion, error) {
	if sug.UserID == 0 {
		return Suggestion{}, fmt.Errorf("suggestion has no author")
	}

	existing, err := s.repo.SuggestionByMessage(ctx, sug.DMChatID, sug.DMMessageID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Suggestion{}, fmt.Errorf("look up suggestion: %w", err)
	}

	if _, err := s.repo.EnsureUser(ctx, sug.UserID); err != nil {
		return Suggestion{}, fmt.Errorf("ensure user: %w", err)
	}

	sug.Status = StatusPending
	sug.CreatedAt = s.now()

	id, err := s.repo.CreateSuggestion(ctx, sug)
	if err != nil {
		return Suggestion{}, fmt.Errorf("create suggestion: %w", err)
	}
	sug.ID = id

	return sug, nil
}

// Decided records what an admin did with a suggestion. It reports whether this
// call is what moved it to approved: a redelivered service message must not hand
// out the same achievement twice.
func (s *Service) Decided(ctx context.Context, chatID int64, messageID int, status Status) (Suggestion, bool, error) {
	sug, err := s.repo.SuggestionByMessage(ctx, chatID, messageID)
	if err != nil {
		return Suggestion{}, false, fmt.Errorf("look up suggestion: %w", err)
	}

	if sug.Status == status {
		return sug, false, nil
	}

	if err := s.repo.SetSuggestionStatus(ctx, sug.ID, status, s.now()); err != nil {
		return Suggestion{}, false, fmt.Errorf("set suggestion status: %w", err)
	}

	newlyApproved := status == StatusApproved
	sug.Status = status

	return sug, newlyApproved, nil
}
