package suggestion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/comment"
	"loudbot/internal/suggestion"
)

var errBoom = errors.New("boom")

const (
	userID    = int64(777)
	dmChatID  = int64(-100777)
	messageID = 55
)

var now = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

type fakeRepo struct {
	byMessage map[int64]suggestion.Suggestion
	nextID    int64

	ensureErr error
	lookupErr error
	createErr error
	statusErr error

	ensured []int64
}

func newRepo() *fakeRepo {
	return &fakeRepo{byMessage: map[int64]suggestion.Suggestion{}}
}

func key(chatID int64, messageID int) int64 { return chatID*1_000_000 + int64(messageID) }

func (r *fakeRepo) EnsureUser(_ context.Context, id int64) (comment.User, error) {
	r.ensured = append(r.ensured, id)

	return comment.User{ID: id}, r.ensureErr
}

func (r *fakeRepo) CreateSuggestion(_ context.Context, s suggestion.Suggestion) (int64, error) {
	if r.createErr != nil {
		return 0, r.createErr
	}

	r.nextID++
	s.ID = r.nextID
	r.byMessage[key(s.DMChatID, s.DMMessageID)] = s

	return s.ID, nil
}

func (r *fakeRepo) SuggestionByMessage(_ context.Context, chatID int64, msgID int) (suggestion.Suggestion, error) {
	if r.lookupErr != nil {
		return suggestion.Suggestion{}, r.lookupErr
	}

	s, ok := r.byMessage[key(chatID, msgID)]
	if !ok {
		return suggestion.Suggestion{}, suggestion.ErrNotFound
	}

	return s, nil
}

func (r *fakeRepo) SetSuggestionStatus(
	_ context.Context, id int64, status suggestion.Status, _ time.Time,
) error {
	if r.statusErr != nil {
		return r.statusErr
	}

	for k, s := range r.byMessage {
		if s.ID == id {
			s.Status = status
			r.byMessage[k] = s
		}
	}

	return nil
}

func newService(repo *fakeRepo) *suggestion.Service {
	svc := suggestion.New(repo)
	svc.SetClock(func() time.Time { return now })

	return svc
}

func offered() suggestion.Suggestion {
	return suggestion.Suggestion{
		UserID:      userID,
		Text:        "предлагаю пост",
		DMChatID:    dmChatID,
		DMMessageID: messageID,
	}
}

func TestOffered(t *testing.T) {
	t.Parallel()

	repo := newRepo()

	got, err := newService(repo).Offered(context.Background(), offered())
	require.NoError(t, err)

	assert.NotZero(t, got.ID)
	assert.Equal(t, suggestion.StatusPending, got.Status)
	assert.Equal(t, now, got.CreatedAt)
	assert.Equal(t, []int64{userID}, repo.ensured, "the user row must exist before the foreign key does")
}

func TestOfferedIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc := newService(repo)
	ctx := context.Background()

	first, err := svc.Offered(ctx, offered())
	require.NoError(t, err)

	// Telegram redelivers updates; the same offer must not become two rows.
	second, err := svc.Offered(ctx, offered())
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)
	assert.Len(t, repo.byMessage, 1)
}

func TestOfferedWithoutAnAuthor(t *testing.T) {
	t.Parallel()

	sug := offered()
	sug.UserID = 0

	_, err := newService(newRepo()).Offered(context.Background(), sug)
	require.Error(t, err, "a suggestion nobody can be credited for is not recorded")
}

func TestOfferedErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		repo func() *fakeRepo
	}{
		{name: "lookup fails", repo: func() *fakeRepo { r := newRepo(); r.lookupErr = errBoom; return r }},
		{name: "ensure user fails", repo: func() *fakeRepo { r := newRepo(); r.ensureErr = errBoom; return r }},
		{name: "insert fails", repo: func() *fakeRepo { r := newRepo(); r.createErr = errBoom; return r }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newService(tc.repo()).Offered(context.Background(), offered())
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestDecidedApproves(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc := newService(repo)
	ctx := context.Background()

	_, err := svc.Offered(ctx, offered())
	require.NoError(t, err)

	sug, approved, err := svc.Decided(ctx, dmChatID, messageID, suggestion.StatusApproved)
	require.NoError(t, err)

	assert.True(t, approved, "this call is what earns the achievement")
	assert.Equal(t, userID, sug.UserID)
	assert.Equal(t, suggestion.StatusApproved, sug.Status)
}

func TestDecidedIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc := newService(repo)
	ctx := context.Background()

	_, err := svc.Offered(ctx, offered())
	require.NoError(t, err)

	_, approved, err := svc.Decided(ctx, dmChatID, messageID, suggestion.StatusApproved)
	require.NoError(t, err)
	require.True(t, approved)

	// A redelivered service message must not hand out the reward twice.
	_, approved, err = svc.Decided(ctx, dmChatID, messageID, suggestion.StatusApproved)
	require.NoError(t, err)
	assert.False(t, approved)
}

func TestDecidedDeclineEarnsNothing(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	svc := newService(repo)
	ctx := context.Background()

	_, err := svc.Offered(ctx, offered())
	require.NoError(t, err)

	sug, approved, err := svc.Decided(ctx, dmChatID, messageID, suggestion.StatusDeclined)
	require.NoError(t, err)
	assert.False(t, approved)
	assert.Equal(t, suggestion.StatusDeclined, sug.Status)
}

func TestDecidedOnAnUnknownSuggestion(t *testing.T) {
	t.Parallel()

	_, _, err := newService(newRepo()).Decided(context.Background(), dmChatID, messageID, suggestion.StatusApproved)
	require.ErrorIs(t, err, suggestion.ErrNotFound)
}
