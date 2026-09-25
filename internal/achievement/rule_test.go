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

			got := tc.rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: "x", At: at(tc.hour)}, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMatchesNoHourWindow(t *testing.T) {
	t.Parallel()

	rule := achievement.Rule{MinLength: ptr(1)}
	assert.True(t, rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: "x", At: at(3)}, nil))
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

			assert.Equal(t, tc.want, tc.rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: tc.text, At: at(12)}, nil))
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

			assert.Equal(t, tc.want, tc.rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: tc.text, At: at(12)}, nil))
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	t.Parallel()

	rule := achievement.Rule{Pattern: `(?i)котик`}
	re := regexp.MustCompile(rule.Pattern)

	assert.True(t, rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: "какой КОТИК", At: at(12)}, re))
	assert.False(t, rule.Matches(achievement.Event{Kind: achievement.KindComment, Text: "какой пёсик", At: at(12)}, re))

	// A rule with no pattern is not filtered by one.
	assert.True(t, achievement.Rule{MinLength: ptr(1)}.Matches(achievement.Event{Kind: achievement.KindComment, Text: "x", At: at(12)}, nil))
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

			event := achievement.Event{Kind: achievement.KindComment, Text: "x", At: at(12), Counters: counters}
			assert.Equal(t, tc.want, tc.rule.Matches(event, nil))
		})
	}
}

func TestMatchesRequiresEveryConditionSet(t *testing.T) {
	t.Parallel()

	// Conditions are "and": the night window holds, the length does not.
	rule := achievement.Rule{AfterHour: ptr(23), BeforeHour: ptr(5), MinLength: ptr(100)}

	event := achievement.Event{Kind: achievement.KindComment, Text: "коротко", At: at(3)}
	assert.False(t, rule.Matches(event, nil))

	rule.MinLength = ptr(3)
	assert.True(t, rule.Matches(event, nil))
}

func TestMatchesEventKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rule achievement.Kind
		got  achievement.Kind
		want bool
	}{
		{name: "comment rule on a comment", rule: achievement.KindComment, got: achievement.KindComment, want: true},
		{name: "comment rule ignores a post", rule: achievement.KindComment, got: achievement.KindPost, want: false},
		{name: "post rule on a post", rule: achievement.KindPost, got: achievement.KindPost, want: true},
		{name: "post rule ignores a comment", rule: achievement.KindPost, got: achievement.KindComment, want: false},
		{name: "any takes a comment", rule: achievement.KindAny, got: achievement.KindComment, want: true},
		{name: "any takes a post", rule: achievement.KindAny, got: achievement.KindPost, want: true},
		{
			// Rules written before suggested posts existed carry no kind and were
			// always about comments.
			name: "an unset kind means comment", rule: "", got: achievement.KindComment, want: true,
		},
		{name: "an unset kind ignores a post", rule: "", got: achievement.KindPost, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rule := achievement.Rule{Event: tc.rule, MinLength: ptr(1)}
			event := achievement.Event{Kind: tc.got, Text: "привет", At: at(12)}

			assert.Equal(t, tc.want, rule.Matches(event, nil))
		})
	}
}

func TestMatchesReadsThePostText(t *testing.T) {
	t.Parallel()

	// The text conditions read whatever the event is about, so a pattern rule on
	// posts inspects the suggested post rather than a comment.
	rule := achievement.Rule{Event: achievement.KindPost, Pattern: `(?i)котик`}
	re := regexp.MustCompile(rule.Pattern)

	post := achievement.Event{Kind: achievement.KindPost, Text: "смотрите какой котик", At: at(12)}
	assert.True(t, rule.Matches(post, re))

	comment := achievement.Event{Kind: achievement.KindComment, Text: "смотрите какой котик", At: at(12)}
	assert.False(t, rule.Matches(comment, re), "the same text on a comment is not this rule's business")
}
