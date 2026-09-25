package achievement_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/achievement"
)

var errBoom = errors.New("boom")

const userID = int64(777)

type fakeRepo struct {
	rules    []achievement.Rule
	counters achievement.Counters
	held     map[int64]bool

	rulesErr    error
	countersErr error
	grantErr    error

	grants []int64
}

func newRepo(rules ...achievement.Rule) *fakeRepo {
	return &fakeRepo{rules: rules, held: map[int64]bool{}}
}

func (r *fakeRepo) ActiveRules(context.Context) ([]achievement.Rule, error) {
	return r.rules, r.rulesErr
}

func (r *fakeRepo) Counters(context.Context, int64) (achievement.Counters, error) {
	return r.counters, r.countersErr
}

func (r *fakeRepo) Grant(_ context.Context, _, achievementID int64) (bool, error) {
	if r.grantErr != nil {
		return false, r.grantErr
	}

	r.grants = append(r.grants, achievementID)

	if r.held[achievementID] {
		return false, nil
	}

	r.held[achievementID] = true

	return true, nil
}

func newAwarder(repo *fakeRepo, loc *time.Location) *achievement.Awarder {
	return achievement.New(repo, loc, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func event() achievement.Event {
	return achievement.Event{Kind: achievement.KindComment, UserID: userID, Text: "привет", At: at(12)}
}

func TestAwardGrantsAMatchingRule(t *testing.T) {
	t.Parallel()

	repo := newRepo(achievement.Rule{
		ID: 1, AchievementID: 10, AchievementCode: "talker", AchievementTitle: "Болтун",
		MinLength: ptr(3),
	})

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err)

	require.Len(t, granted, 1)
	assert.Equal(t, "Болтун", granted[0].Title)
	assert.Equal(t, []int64{10}, repo.grants)
}

func TestAwardSkipsRulesThatDoNotMatch(t *testing.T) {
	t.Parallel()

	repo := newRepo(
		achievement.Rule{ID: 1, AchievementID: 10, MinLength: ptr(3)},
		achievement.Rule{ID: 2, AchievementID: 20, MinLength: ptr(100)},
	)

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err)

	require.Len(t, granted, 1)
	assert.Equal(t, []int64{10}, repo.grants)
}

func TestAwardDoesNotRepeatWhatIsAlreadyHeld(t *testing.T) {
	t.Parallel()

	repo := newRepo(achievement.Rule{ID: 1, AchievementID: 10, MinLength: ptr(3)})
	repo.held[10] = true

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err)
	assert.Empty(t, granted, "a rule that keeps matching must not notify twice")
}

func TestAwardReadsCountersItself(t *testing.T) {
	t.Parallel()

	repo := newRepo(achievement.Rule{ID: 1, AchievementID: 10, MinComments: ptr(10)})
	repo.counters = achievement.Counters{Comments: 10}

	// The event carries no counters: the awarder reads them, so they include the
	// comment that just triggered this check.
	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err)
	assert.Len(t, granted, 1)
}

func TestAwardReadsTheClockInTheConfiguredZone(t *testing.T) {
	t.Parallel()

	moscow := time.FixedZone("MSK", 3*60*60)

	// 23:30 UTC is 02:30 in Moscow, which is inside a 23..5 night window.
	repo := newRepo(achievement.Rule{ID: 1, AchievementID: 10, AfterHour: ptr(23), BeforeHour: ptr(5)})

	e := event()
	e.At = time.Date(2026, 9, 25, 23, 30, 0, 0, time.UTC)

	granted, err := newAwarder(repo, moscow).Award(context.Background(), e)
	require.NoError(t, err)
	assert.Len(t, granted, 1)

	// The same instant in UTC is 23:30, also inside the window — so check an hour
	// that only the shift can reach: 06:30 UTC is 09:30 MSK, outside the night.
	e.At = time.Date(2026, 9, 25, 6, 30, 0, 0, time.UTC)

	repo2 := newRepo(achievement.Rule{ID: 1, AchievementID: 10, AfterHour: ptr(23), BeforeHour: ptr(5)})
	granted, err = newAwarder(repo2, moscow).Award(context.Background(), e)
	require.NoError(t, err)
	assert.Empty(t, granted)

	// In UTC that same instant is 06:30, also outside — but 02:30 UTC is inside.
	e.At = time.Date(2026, 9, 25, 2, 30, 0, 0, time.UTC)

	repo3 := newRepo(achievement.Rule{ID: 1, AchievementID: 10, AfterHour: ptr(23), BeforeHour: ptr(5)})
	granted, err = newAwarder(repo3, moscow).Award(context.Background(), e)
	require.NoError(t, err)
	assert.Empty(t, granted, "02:30 UTC is 05:30 in Moscow, just past the window")
}

func TestAwardSkipsARuleWithABrokenPattern(t *testing.T) {
	t.Parallel()

	repo := newRepo(
		achievement.Rule{ID: 1, AchievementID: 10, Pattern: "("},
		achievement.Rule{ID: 2, AchievementID: 20, MinLength: ptr(3)},
	)

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err, "a bad row must not stop people from commenting")

	require.Len(t, granted, 1)
	assert.Equal(t, []int64{20}, repo.grants, "only the broken rule is skipped")
}

func TestAwardWithNoRules(t *testing.T) {
	t.Parallel()

	repo := newRepo()

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), event())
	require.NoError(t, err)
	assert.Empty(t, granted)
	assert.Equal(t, achievement.Counters{}, repo.counters, "no rules means no counter query")
}

func TestAwardErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		repo func() *fakeRepo
	}{
		{
			name: "rules cannot be read",
			repo: func() *fakeRepo { r := newRepo(); r.rulesErr = errBoom; return r },
		},
		{
			name: "counters cannot be read",
			repo: func() *fakeRepo {
				r := newRepo(achievement.Rule{ID: 1, AchievementID: 10, MinLength: ptr(1)})
				r.countersErr = errBoom

				return r
			},
		},
		{
			name: "grant fails",
			repo: func() *fakeRepo {
				r := newRepo(achievement.Rule{ID: 1, AchievementID: 10, MinLength: ptr(1)})
				r.grantErr = errBoom

				return r
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newAwarder(tc.repo(), time.UTC).Award(context.Background(), event())
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestAwardCompilesAPatternOnce(t *testing.T) {
	t.Parallel()

	repo := newRepo(achievement.Rule{ID: 1, AchievementID: 10, Pattern: `(?i)котик`})
	awarder := newAwarder(repo, time.UTC)

	e := event()
	e.Text = "какой котик"

	ctx := context.Background()
	for range 3 {
		_, err := awarder.Award(ctx, e)
		require.NoError(t, err)
	}

	assert.Len(t, repo.grants, 3, "the rule is checked every time")
	assert.Len(t, repo.held, 1, "but the achievement is handed out once")
}

func TestAwardSeparatesPostsFromComments(t *testing.T) {
	t.Parallel()

	repo := newRepo(
		achievement.Rule{ID: 1, Event: achievement.KindComment, AchievementID: 10, MinLength: ptr(1)},
		achievement.Rule{ID: 2, Event: achievement.KindPost, AchievementID: 20, MinLength: ptr(1)},
		achievement.Rule{ID: 3, Event: achievement.KindAny, AchievementID: 30, MinLength: ptr(1)},
	)
	awarder := newAwarder(repo, time.UTC)

	post := event()
	post.Kind = achievement.KindPost

	granted, err := awarder.Award(context.Background(), post)
	require.NoError(t, err)

	assert.Equal(t, []int64{20, 30}, repo.grants, "a comment-only rule stays out of a post's way")
	assert.Len(t, granted, 2)
}

func TestAwardOnAnApprovedPostCountsPosts(t *testing.T) {
	t.Parallel()

	// A "five approved posts" rule has to fire on the approval itself, which is
	// the whole reason a rule carries an event kind.
	repo := newRepo(achievement.Rule{
		ID: 1, Event: achievement.KindPost, AchievementID: 10,
		AchievementTitle: "Автор", MinPosts: ptr(5),
	})
	repo.counters = achievement.Counters{Posts: 5}

	post := event()
	post.Kind = achievement.KindPost

	granted, err := newAwarder(repo, time.UTC).Award(context.Background(), post)
	require.NoError(t, err)
	require.Len(t, granted, 1)
	assert.Equal(t, "Автор", granted[0].Title)
}
