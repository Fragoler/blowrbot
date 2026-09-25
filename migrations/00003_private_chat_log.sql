-- +goose Up

-- What the bot and one person have said to each other in private. The Bot API
-- cannot read a chat's history, so a message that is not recorded here can never
-- be deleted later; this table is what lets a new flow wipe the slate clean.
--
-- It is transport bookkeeping rather than domain data: no foreign key, because a
-- message is recorded the moment it appears, which can be before the user row
-- exists. Rows are removed as soon as their messages are deleted.
CREATE TABLE private_messages (
    user_id    BIGINT      NOT NULL,
    message_id INTEGER     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, message_id)
);

-- +goose Down

DROP TABLE private_messages;
