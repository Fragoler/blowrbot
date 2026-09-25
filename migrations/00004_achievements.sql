-- +goose Up

-- The mask list moves out of config.toml: a nickname is now a row, because
-- achievements point at it. A label that nothing points at is public; one that an
-- achievement unlocks is offered only to users who hold that achievement.
--
-- The CHECK is not cosmetic: a label travels inside Telegram's callback_data,
-- which is capped at 64 bytes, and "nick:" already takes five of them. Without it
-- a hand-added long label would render a button that silently does nothing.
CREATE TABLE nicknames (
    id        BIGSERIAL PRIMARY KEY,
    code      TEXT    NOT NULL UNIQUE,
    label     TEXT    NOT NULL UNIQUE CHECK (octet_length(label) BETWEEN 1 AND 59),
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE achievements (
    id          BIGSERIAL PRIMARY KEY,
    -- code is the stable handle used when granting one by hand.
    code        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which masks an achievement unlocks. Many-to-many on purpose: one achievement
-- may open several masks, and a mask may be reachable through any of several.
CREATE TABLE achievement_nicknames (
    achievement_id BIGINT NOT NULL REFERENCES achievements (id) ON DELETE CASCADE,
    nickname_id    BIGINT NOT NULL REFERENCES nicknames (id) ON DELETE CASCADE,
    PRIMARY KEY (achievement_id, nickname_id)
);

-- Read the other way round when deciding what one user may wear.
CREATE INDEX achievement_nicknames_nickname_idx ON achievement_nicknames (nickname_id);

CREATE TABLE user_achievements (
    user_id        BIGINT      NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    achievement_id BIGINT      NOT NULL REFERENCES achievements (id) ON DELETE CASCADE,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, achievement_id)
);

-- The masks that used to be the config defaults, all public to start with.
INSERT INTO nicknames (code, label) VALUES
    ('ghost', 'Призрак T-корпуса'),
    ('sambo', 'Потный самбист'),
    ('emo', 'Секси имошница'),
    ('tester', 'Тестировщик реактора'),
    ('misterP', 'Мистер пенис');

INSERT INTO achievements (code, title, description) VALUES
    ('tester', 'Тестировщик', 'Будни QA'),
    ('misterP', 'Мистер П', 'Добрый день, коллеги!');

INSERT INTO achievement_nicknames (achievement_id, nickname_id)
    SELECT a.id, n.id
    FROM achievements a
    CROSS JOIN nicknames n 
    WHERE 
        (a.code = 'tester' AND n.code = 'tester') OR
        (a.code = 'misterP' AND n.code = 'misterP');

-- +goose Down

DROP TABLE user_achievements;
DROP TABLE achievement_nicknames;
DROP TABLE achievements;
DROP TABLE nicknames;
