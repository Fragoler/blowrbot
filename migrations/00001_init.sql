-- +goose Up

-- The nickname list lives in config.toml, not here: editing it is a config change
-- and a restart, with no migration. Every table below therefore stores the chosen
-- label as plain TEXT rather than a foreign key, so a mask dropped from the config
-- never rewrites or breaks a message already published under it.
CREATE TABLE users (
    user_id       BIGINT PRIMARY KEY,
    first_seen    TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_banned     BOOLEAN     NOT NULL DEFAULT FALSE,
    last_nickname TEXT
);

-- posts links a channel post to its auto-forwarded copy in the discussion group;
-- replying to that copy is what puts a comment into the post's thread.
CREATE TABLE posts (
    channel_message_id    INTEGER PRIMARY KEY,
    discussion_chat_id    BIGINT      NOT NULL,
    discussion_message_id INTEGER     NOT NULL,
    invite_message_id     INTEGER,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE comment_drafts (
    user_id    BIGINT PRIMARY KEY REFERENCES users (user_id) ON DELETE CASCADE,
    post_id    INTEGER     NOT NULL,
    nickname   TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE comments (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT      NOT NULL REFERENCES users (user_id),
    post_id             INTEGER     NOT NULL,
    nickname            TEXT        NOT NULL,
    message_id_in_group INTEGER,
    content_text        TEXT        NOT NULL DEFAULT '',
    media_json          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    status              TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX comments_post_idx ON comments (post_id);

CREATE UNIQUE INDEX comments_message_idx ON comments (message_id_in_group) WHERE message_id_in_group IS NOT NULL;

CREATE TABLE suggested_posts (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (user_id),
    content_text TEXT        NOT NULL DEFAULT '',
    media_json   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    status       TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by   BIGINT,
    decided_at   TIMESTAMPTZ,
    ml_score     DOUBLE PRECISION
);

CREATE TABLE reports (
    id               BIGSERIAL PRIMARY KEY,
    target_type      TEXT        NOT NULL,
    target_id        BIGINT      NOT NULL,
    reporter_user_id BIGINT      NOT NULL,
    reason           TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_by      BIGINT,
    resolved_at      TIMESTAMPTZ
);

-- identity_map is the private layer: it is the only place linking a published
-- message back to its author. Read access belongs to moderators only.
CREATE TABLE identity_map (
    message_id_in_group INTEGER PRIMARY KEY,
    comment_id          BIGINT      NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
    user_id             BIGINT      NOT NULL REFERENCES users (user_id),
    nickname            TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor_id    BIGINT      NOT NULL,
    action      TEXT        NOT NULL,
    target_type TEXT        NOT NULL,
    target_id   BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE audit_log;
DROP TABLE identity_map;
DROP TABLE reports;
DROP TABLE suggested_posts;
DROP TABLE comments;
DROP TABLE comment_drafts;
DROP TABLE posts;
DROP TABLE users;
