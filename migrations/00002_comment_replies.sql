-- +goose Up

-- A reply is an ordinary comment that also points at the one it answers. The key
-- is self-referencing, and ON DELETE SET NULL keeps a branch alive when the
-- comment above it is removed by moderation.
ALTER TABLE comments
    ADD COLUMN reply_to_comment_id BIGINT REFERENCES comments (id) ON DELETE SET NULL;

-- Set while the author is writing, when they arrived through the "answer"
-- link under a comment rather than the button under the post.
ALTER TABLE comment_drafts
    ADD COLUMN reply_to_comment_id BIGINT;

-- +goose Down

ALTER TABLE comment_drafts DROP COLUMN reply_to_comment_id;
ALTER TABLE comments DROP COLUMN reply_to_comment_id;
