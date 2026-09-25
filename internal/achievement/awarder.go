package achievement

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"
)

// Repository reads the rules and a person's totals, and records a grant.
type Repository interface {
	// ActiveRules returns the rules currently in force. It is read on every
	// published comment, so a rule edited in the database takes effect at once.
	ActiveRules(ctx context.Context) ([]Rule, error)
	Counters(ctx context.Context, userID int64) (Counters, error)
	// Grant records the achievement and reports whether it was new.
	Grant(ctx context.Context, userID, achievementID int64) (bool, error)
}

// Granted is an achievement a comment just earned, for telling its author.
type Granted struct {
	Code        string
	Title       string
	Description string
}

// Awarder is the single place where a comment turns into achievements.
type Awarder struct {
	repo     Repository
	location *time.Location
	log      *slog.Logger

	// patterns caches compiled regexps across evaluations: rules come from the
	// database on every comment, but their patterns rarely change.
	mu       sync.Mutex
	patterns map[string]*regexp.Regexp
}

func New(repo Repository, location *time.Location, log *slog.Logger) *Awarder {
	if location == nil {
		location = time.UTC
	}

	return &Awarder{
		repo:     repo,
		location: location,
		log:      log,
		patterns: map[string]*regexp.Regexp{},
	}
}

// Award checks every active rule against a freshly published comment and grants
// what matches. Counters are read here, so they include this comment.
//
// It never reports a rule's own failure as the caller's: a broken pattern
// disables that one rule and is logged, because a bad row in the database must
// not stop people from commenting.
func (a *Awarder) Award(ctx context.Context, e Event) ([]Granted, error) {
	rules, err := a.repo.ActiveRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("active rules: %w", err)
	}

	if len(rules) == 0 {
		return nil, nil
	}

	counters, err := a.repo.Counters(ctx, e.UserID)
	if err != nil {
		return nil, fmt.Errorf("counters: %w", err)
	}

	e.Counters = counters
	e.At = e.At.In(a.location)

	var granted []Granted

	a.log.Debug("checking achievement rules", slog.Int64("user_id", e.UserID), slog.String("text", e.Text))

	for _, rule := range rules {
		pattern, err := a.pattern(rule.Pattern)
		if err != nil {
			a.log.Error("achievement rule has a broken pattern",
				slog.Int64("rule_id", rule.ID),
				slog.String("pattern", rule.Pattern),
				slog.Any("error", err),
			)

			continue
		}

		matched := rule.Matches(e, pattern)
		if !matched {
			continue
		}
		fresh, err := a.repo.Grant(ctx, e.UserID, rule.AchievementID)
		if err != nil {
			return granted, fmt.Errorf("grant achievement %d: %w", rule.AchievementID, err)
		}

		if !fresh {
			continue
		}

		a.log.Info("achievement granted",
			slog.Int64("user_id", e.UserID),
			slog.Int64("rule_id", rule.ID),
			slog.String("achievement", rule.AchievementCode),
		)

		granted = append(granted, Granted{
			Code:        rule.AchievementCode,
			Title:       rule.AchievementTitle,
			Description: rule.AchievementDescription,
		})
	}

	return granted, nil
}

// pattern compiles a rule's regexp once and reuses it afterwards.
func (a *Awarder) pattern(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if compiled, ok := a.patterns[expr]; ok {
		return compiled, nil
	}

	compiled, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}

	a.patterns[expr] = compiled

	return compiled, nil
}
