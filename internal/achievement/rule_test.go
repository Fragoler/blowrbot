package achievement_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"loudbot/internal/achievement"
)

func ptr[T any](v T) *T { return &v }

func at(hour int) time.Time {
	return time.Date(2026, 9, 25, hour, 30, 0, 0, time.UTC)
}

func TestMatchesHourWindow(t *testing.T) {
	t.Parallel()

	day := achievement.Rule{AfterHour: ptr(9), BeforeHour: ptr(18)}
	night := achievement.Rule{AfterHour: ptr(23), BeforeHour: ptr(5)}

	cases := []struct {
		name string
		rule achievement.Rule
		hour int
		want bool
	}{
		{name: "inside the day window", rule: day, hour: 12, want: true},
		{name: "on the opening hour", rule: day, hour: 9, want: true},
		{name: "on the closing hour is outside", rule: day, hour: 18, want: false},
		{name: "before the day window", rule: day, hour: 8, want: false},
		{name: "night wraps past midnight", rule: night, hour: 23, want: true},
		{name: "night covers the small hours", rule: night, hour: 2, want: true},
		{name: "night ends at five", rule: night, hour: 5, want: false},
		{name: "midday is not night", rule: night, hour: 12, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.rule.Matches(achievement.Event{Text: "x", At: at(tc.hour)}, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMatchesNoHourWindow(t *testing.T) {
	t.Parallel()

	rule := achievement.Rule{MinLength: ptr(1)}
	assert.True(t, rule.Matches(achievement.Event{Text: "x", At: at(3)}, nil))
}

func TestMatchesLength(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rule achievement.Rule
		text string
		want bool
	}{
		{name: "long enough", rule: achievement.Rule{MinLength: ptr(5)}, text: "привет", want: true},
		{name: "exactly at the floor", rule: achievement.Rule{MinLength: ptr(6)}, text: "привет", want: true},
		{name: "too short", rule: achievement.Rule{MinLength: ptr(7)}, text: "привет", want: false},
		{name: "short enough", rule: achievement.Rule{MaxLength: ptr(6)}, text: "привет", want: true},
		{name: "too long", rule: achievement.Rule{MaxLength: ptr(5)}, text: "привет", want: false},
		{
			// Cyrillic is two bytes a letter: the rule counts runes, not bytes.
			name: "counts runes, not bytes",
			rule: achievement.Rule{MaxLength: ptr(6)},
			text: "привет",
			want: true,
		},
		{name: "trims before counting", rule: achievement.Rule{MaxLength: ptr(6)}, text: "  привет  ", want: true},
		{
			name: "a band needs both ends to hold",
			rule: achievement.Rule{MinLength: ptr(3), MaxLength: ptr(5)},
			text: "привет", want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.rule.Matches(achievement.Event{Text: tc.text, At: at(12)}, nil))
		})
	}
}

func TestMatchesUpperOnly(t *testing.T) {
	t.Parallel()

	shouting := achievement.Rule{UpperOnly: ptr(true)}
	quiet := achievement.Rule{UpperOnly: ptr(false)}

	cases := []struct {
		name string
		rule achievement.Rule
		text string
		want bool
	}{
		{name: "all caps", rule: shouting, text: "ЧТО ПРОИСХОДИТ", want: true},
		{name: "caps with punctuation and digits", rule: shouting, text: "ЧТО?! 100%", want: true},
		{name: "mixed case is not shouting", rule: shouting, text: "Что происходит", want: false},
		{name: "one lower letter is enough to disqualify", rule: shouting, text: "ЧТо", want: false},
		{name: "no letters at all is not shouting", rule: shouting, text: "123 !!!", want: false},
		{name: "the inverse rule matches quiet text", rule: quiet, text: "привет", want: true},
		{name: "the inverse rule rejects shouting", rule: quiet, text: "ПРИВЕТ", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.rule.Matches(achievement.Event{Text: tc.text, At: at(12)}, nil))
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	t.Parallel()

	rule := achievement.Rule{Pattern: `(?i)котик`}
	re := regexp.MustCompile(rule.Pattern)

	assert.True(t, rule.Matches(achievement.Event{Text: "какой КОТИК", At: at(12)}, re))
	assert.False(t, rule.Matches(achievement.Event{Text: "какой пёсик", At: at(12)}, re))

	// A rule with no pattern is not filtered by one.
	assert.True(t, achievement.Rule{MinLength: ptr(1)}.Matches(achievement.Event{Text: "x", At: at(12)}, nil))
}

func TestMatchesCounters(t *testing.T) {
	t.Parallel()

	counters := achievement.Counters{Comments: 10, Replies: 3, Posts: 1, Achievements: 2}

	cases := []struct {
		name string
		rule achievement.Rule
		want bool
	}{
		{name: "comments reached", rule: achievement.Rule{MinComments: ptr(10)}, want: true},
		{name: "comments short", rule: achievement.Rule{MinComments: ptr(11)}, want: false},
		{name: "replies reached", rule: achievement.Rule{MinReplies: ptr(3)}, want: true},
		{name: "replies short", rule: achievement.Rule{MinReplies: ptr(4)}, want: false},
		{name: "posts reached", rule: achievement.Rule{MinPosts: ptr(1)}, want: true},
		{name: "posts short", rule: achievement.Rule{MinPosts: ptr(2)}, want: false},
		{name: "achievements reached", rule: achievement.Rule{MinAchievements: ptr(2)}, want: true},
		{name: "achievements short", rule: achievement.Rule{MinAchievements: ptr(3)}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			event := achievement.Event{Text: "x", At: at(12), Counters: counters}
			assert.Equal(t, tc.want, tc.rule.Matches(event, nil))
		})
	}
}

func TestMatchesRequiresEveryConditionSet(t *testing.T) {
	t.Parallel()

	// Conditions are "and": the night window holds, the length does not.
	rule := achievement.Rule{AfterHour: ptr(23), BeforeHour: ptr(5), MinLength: ptr(100)}

	event := achievement.Event{Text: "коротко", At: at(3)}
	assert.False(t, rule.Matches(event, nil))

	rule.MinLength = ptr(3)
	assert.True(t, rule.Matches(event, nil))
}
