-- +goose Up

-- Moderation of suggestions happens in Telegram's own Direct Messages interface,
-- so the bot never decides anything: it records what was offered and then listens
-- for the service message that says what an admin did. These two columns are how
-- a decision is matched back to the row — the service message carries the
-- original suggestion message, not our id.
ALTER TABLE suggested_posts
    ADD COLUMN dm_chat_id    BIGINT,
    ADD COLUMN dm_message_id INTEGER;

CREATE UNIQUE INDEX suggested_posts_message_idx
    ON suggested_posts (dm_chat_id, dm_message_id)
    WHERE dm_chat_id IS NOT NULL;

-- Which event a rule reacts to. Without this a "five approved posts" rule would
-- sit idle until its author happened to write a comment, because that was the
-- only moment rules were ever checked.
ALTER TABLE achievement_rules
    ADD COLUMN event TEXT NOT NULL DEFAULT 'comment'
    CHECK (event IN ('comment', 'post', 'any'));

-- +goose Down

ALTER TABLE achievement_rules DROP COLUMN event;
DROP INDEX suggested_posts_message_idx;
ALTER TABLE suggested_posts DROP COLUMN dm_message_id, DROP COLUMN dm_chat_id;
