// Package achievement decides, in one place, which achievements a comment earns.
// Every trigger lives here: nothing elsewhere in the codebase inspects a comment
// to hand out a reward, and the only entry point is Awarder.Award.
package achievement

import (
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Kind is what happened. A rule reacts to one kind, or to both.
type Kind string

const (
	KindComment Kind = "comment"
	KindPost    Kind = "post"
	// KindAny is only ever written on a rule, never on an event.
	KindAny Kind = "any"
)

// Counters are a person's totals at the moment a rule is checked. They already
// include the comment that triggered the check, so a "ten comments" rule fires on
// the tenth rather than the eleventh.
type Counters struct {
	Comments     int
	Replies      int
	Posts        int
	Achievements int
}

// Event is something a person just did — a comment published, or a suggested
// post approved — offered to every rule that reacts to its kind.
type Event struct {
	Kind   Kind
	UserID int64
	// Text is the comment's body, or the suggested post's, so the text conditions
	// read whatever the event is actually about.
	Text string
	// At is the publication time, already in the configured timezone.
	At       time.Time
	IsReply  bool
	Counters Counters
}

// Rule is one row of achievement_rules. A nil condition is not checked; the ones
// that are set must all hold, so a rule reads as "and" down its filled columns.
type Rule struct {
	ID int64
	// Event is the kind this rule reacts to: comment, post, or any.
	Event                  Kind
	AchievementID          int64
	AchievementCode        string
	AchievementTitle       string
	AchievementDescription string

	AfterHour  *int
	BeforeHour *int

	MinLength *int
	MaxLength *int
	UpperOnly *bool
	Pattern   string

	MinComments     *int
	MinReplies      *int
	MinPosts        *int
	MinAchievements *int
}

// Matches reports whether the event satisfies every condition the rule sets.
// pattern is the rule's compiled regexp, or nil when it has none.
func (r Rule) Matches(e Event, pattern *regexp.Regexp) bool {
	if !r.reactsTo(e.Kind) {
		return false
	}

	text := strings.TrimSpace(e.Text)
	length := utf8.RuneCountInString(text)

	if !r.withinHours(e.At) {
		slog.Debug("rule miss: hours", "rule_id", r.ID, "at", e.At)
		return false
	}
	if r.MinLength != nil && length < *r.MinLength {
		slog.Debug("rule miss: min length", "rule_id", r.ID, "length", length)
		return false
	}
	if r.MaxLength != nil && length > *r.MaxLength {
		slog.Debug("rule miss: max length", "rule_id", r.ID, "length", length)
		return false
	}
	if r.UpperOnly != nil && *r.UpperOnly != isUpper(text) {
		slog.Debug("rule miss: upper only", "rule_id", r.ID, "isUpper", isUpper(text))
		return false
	}
	if pattern != nil && !pattern.MatchString(text) {
		slog.Debug("rule miss: pattern", "rule_id", r.ID, "pattern", r.Pattern, "text", text)
		return false
	}
	if r.MinComments != nil && e.Counters.Comments < *r.MinComments {
		slog.Debug("rule miss: min comments", "rule_id", r.ID, "have", e.Counters.Comments, "need", *r.MinComments)
		return false
	}
	if r.MinReplies != nil && e.Counters.Replies < *r.MinReplies {
		slog.Debug("rule miss: min replies", "rule_id", r.ID)
		return false
	}
	if r.MinPosts != nil && e.Counters.Posts < *r.MinPosts {
		slog.Debug("rule miss: min posts", "rule_id", r.ID)
		return false
	}
	if r.MinAchievements != nil && e.Counters.Achievements < *r.MinAchievements {
		slog.Debug("rule miss: min achievements", "rule_id", r.ID)
		return false
	}
	return true
}

// reactsTo reports whether the rule cares about this kind of event. An empty
// Event on a rule means "comment", which is what every rule written before
// suggested posts existed meant.
func (r Rule) reactsTo(kind Kind) bool {
	switch r.Event {
	case KindAny:
		return true
	case "":
		return kind == KindComment
	default:
		return r.Event == kind
	}
}

// withinHours checks the half-open window [AfterHour, BeforeHour). A window whose
// start is past its end wraps midnight, which is how "at night" is expressed.
func (r Rule) withinHours(at time.Time) bool {
	if r.AfterHour == nil || r.BeforeHour == nil {
		return true
	}

	hour, after, before := at.Hour(), *r.AfterHour, *r.BeforeHour

	if after <= before {
		return hour >= after && hour < before
	}

	return hour >= after || hour < before
}

// isUpper reports whether the text shouts: it has letters, and none is lower
// case. Digits, spaces and punctuation are ignored, so "ЧТО?! 100%" counts.
func isUpper(text string) bool {
	letters := false

	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}

		if unicode.IsLower(r) {
			return false
		}

		letters = true
	}

	return letters
}
