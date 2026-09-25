-- +goose Up

-- Rules that hand out achievements on their own. One row is one rule: every
-- condition it sets must hold, and a NULL condition is simply not checked. That
-- keeps a rule readable when written by hand — the columns that matter are the
-- ones that are filled in.
--
-- The hour window is half-open, [after_hour, before_hour), in the timezone from
-- service.timezone. after > before wraps midnight, so 23..5 is "at night".
CREATE TABLE achievement_rules (
    id             BIGSERIAL PRIMARY KEY,
    achievement_id BIGINT  NOT NULL REFERENCES achievements (id) ON DELETE CASCADE,
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,

    after_hour  SMALLINT CHECK (after_hour BETWEEN 0 AND 23),
    before_hour SMALLINT CHECK (before_hour BETWEEN 0 AND 23),

    min_length INTEGER CHECK (min_length >= 0),
    max_length INTEGER CHECK (max_length >= 0),
    upper_only BOOLEAN,
    -- Go regexp (RE2): no backtracking, so a pattern cannot hang the bot. A
    -- pattern that fails to compile disables its own rule and is logged.
    pattern TEXT,

    min_comments     INTEGER CHECK (min_comments >= 0),
    min_replies      INTEGER CHECK (min_replies >= 0),
    min_posts        INTEGER CHECK (min_posts >= 0),
    min_achievements INTEGER CHECK (min_achievements >= 0),

    -- A rule with no condition at all would fire on every comment.
    CONSTRAINT achievement_rules_not_empty CHECK (
        num_nonnulls(after_hour, before_hour, min_length, max_length, upper_only,
                     pattern, min_comments, min_replies, min_posts, min_achievements) > 0
    ),
    -- An hour window needs both ends.
    CONSTRAINT achievement_rules_hours_paired CHECK (
        num_nonnulls(after_hour, before_hour) <> 1
    )
);

CREATE INDEX achievement_rules_active_idx ON achievement_rules (achievement_id) WHERE is_active;

-- First rules
INSERT INTO achievement_rules (achievement_id, pattern) 
    SELECT id, '(?i)пенис' FROM achievements WHERE code = 'misterP';



-- +goose Down

DROP TABLE achievement_rules;
