package profile_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/comment"
	"loudbot/internal/profile"
)

var errBoom = errors.New("boom")

const userID = int64(777)

type fakeRepo struct {
	achievements []profile.Achievement
	activity     profile.Activity
	nicknames    []comment.Nickname

	ensureErr       error
	achievementsErr error
	activityErr     error
	nicknamesErr    error
}

func (r *fakeRepo) EnsureUser(context.Context, int64) (comment.User, error) {
	return comment.User{ID: userID}, r.ensureErr
}

func (r *fakeRepo) Achievements(context.Context, int64) ([]profile.Achievement, error) {
	return r.achievements, r.achievementsErr
}

func (r *fakeRepo) Activity(context.Context, int64) (profile.Activity, error) {
	return r.activity, r.activityErr
}

func (r *fakeRepo) Nicknames(context.Context, int64) ([]comment.Nickname, error) {
	return r.nicknames, r.nicknamesErr
}

func TestGet(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{
		activity:     profile.Activity{Comments: 4, Replies: 2},
		achievements: []profile.Achievement{{Code: "first", Title: "Первый"}},
		nicknames:    []comment.Nickname{{Label: "Лис"}, {Label: "Мамонт"}},
	}

	got, err := profile.New(repo).Get(context.Background(), userID)
	require.NoError(t, err)

	assert.Equal(t, profile.Activity{Comments: 4, Replies: 2}, got.Activity)
	assert.Equal(t, "Первый", got.Achievements[0].Title)
	assert.Len(t, got.Nicknames, 2, "unlocked masks are listed alongside the public ones")
}

func TestGetForSomeoneWhoHasWrittenNothing(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{nicknames: []comment.Nickname{{Label: "Лис"}}}

	got, err := profile.New(repo).Get(context.Background(), userID)
	require.NoError(t, err, "an empty profile is not an error")

	assert.Zero(t, got.Activity.Comments)
	assert.Empty(t, got.Achievements)
	assert.Len(t, got.Nicknames, 1, "the public masks are still offered")
}

func TestGetErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		repo *fakeRepo
	}{
		{name: "user lookup fails", repo: &fakeRepo{ensureErr: errBoom}},
		{name: "activity fails", repo: &fakeRepo{activityErr: errBoom}},
		{name: "achievements fail", repo: &fakeRepo{achievementsErr: errBoom}},
		{name: "nicknames fail", repo: &fakeRepo{nicknamesErr: errBoom}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := profile.New(tc.repo).Get(context.Background(), userID)
			require.ErrorIs(t, err, errBoom)
		})
	}
}
