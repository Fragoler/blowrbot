// Package storage implements comment.Repository on top of Postgres.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"loudbot/internal/comment"
	"loudbot/internal/config"
)

type Storage struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, cfg config.Postgres, log *slog.Logger) (*Storage, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Storage{pool: pool, log: log}, nil
}

func (s *Storage) Close() { s.pool.Close() }

// notFound maps a missing row onto the sentinel the comment package expects.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return comment.ErrNotFound
	}

	return err
}

func (s *Storage) EnsureUser(ctx context.Context, userID int64) (comment.User, error) {
	const q = `
INSERT INTO users (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING user_id, is_banned, COALESCE(last_nickname_id, 0)`

	var u comment.User
	if err := s.pool.QueryRow(ctx, q, userID).Scan(&u.ID, &u.Banned, &u.LastNicknameID); err != nil {
		return comment.User{}, fmt.Errorf("ensure user %d: %w", userID, err)
	}

	return u, nil
}

func (s *Storage) SetLastNickname(ctx context.Context, userID, nicknameID int64) error {
	const q = `UPDATE users SET last_nickname_id = $2 WHERE user_id = $1`

	if _, err := s.pool.Exec(ctx, q, userID, nicknameID); err != nil {
		return fmt.Errorf("set last nickname: %w", err)
	}

	return nil
}

func (s *Storage) ActiveNicknames(ctx context.Context) ([]comment.Nickname, error) {
	const q = `SELECT id, label, emoji, is_active FROM nicknames WHERE is_active ORDER BY id`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("active nicknames: %w", err)
	}
	defer rows.Close()

	var out []comment.Nickname
	for rows.Next() {
		var n comment.Nickname
		if err := rows.Scan(&n.ID, &n.Label, &n.Emoji, &n.Active); err != nil {
			return nil, fmt.Errorf("scan nickname: %w", err)
		}
		out = append(out, n)
	}

	return out, rows.Err()
}

func (s *Storage) Nickname(ctx context.Context, id int64) (comment.Nickname, error) {
	const q = `SELECT id, label, emoji, is_active FROM nicknames WHERE id = $1`

	var n comment.Nickname
	if err := s.pool.QueryRow(ctx, q, id).Scan(&n.ID, &n.Label, &n.Emoji, &n.Active); err != nil {
		return comment.Nickname{}, notFound(err)
	}

	return n, nil
}

// LinkPost records the discussion-group copy of a channel post. It is called from
// the auto-forward update, which may arrive more than once.
func (s *Storage) LinkPost(ctx context.Context, post comment.Post) error {
	const q = `
INSERT INTO posts (channel_message_id, discussion_chat_id, discussion_message_id)
VALUES ($1, $2, $3)
ON CONFLICT (channel_message_id) DO UPDATE
SET discussion_chat_id = EXCLUDED.discussion_chat_id,
    discussion_message_id = EXCLUDED.discussion_message_id`

	if _, err := s.pool.Exec(ctx, q, post.ChannelMessageID, post.DiscussionChatID, post.DiscussionMessageID); err != nil {
		return fmt.Errorf("link post %d: %w", post.ChannelMessageID, err)
	}

	return nil
}

func (s *Storage) Post(ctx context.Context, channelMessageID int) (comment.Post, error) {
	const q = `
SELECT channel_message_id, discussion_chat_id, discussion_message_id,
       COALESCE(invite_message_id, 0), created_at
FROM posts WHERE channel_message_id = $1`

	var p comment.Post
	err := s.pool.QueryRow(ctx, q, channelMessageID).
		Scan(&p.ChannelMessageID, &p.DiscussionChatID, &p.DiscussionMessageID, &p.InviteMessageID, &p.CreatedAt)
	if err != nil {
		return comment.Post{}, notFound(err)
	}

	return p, nil
}

// MarkInvitePosted records the bot's first comment so it is never posted twice.
func (s *Storage) MarkInvitePosted(ctx context.Context, channelMessageID, inviteMessageID int) error {
	const q = `UPDATE posts SET invite_message_id = $2 WHERE channel_message_id = $1`

	if _, err := s.pool.Exec(ctx, q, channelMessageID, inviteMessageID); err != nil {
		return fmt.Errorf("mark invite for post %d: %w", channelMessageID, err)
	}

	return nil
}

func (s *Storage) SaveDraft(ctx context.Context, draft comment.Draft) error {
	const q = `
INSERT INTO comment_drafts (user_id, post_id, nickname_id, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET post_id = EXCLUDED.post_id,
    nickname_id = EXCLUDED.nickname_id,
    created_at = EXCLUDED.created_at`

	if _, err := s.pool.Exec(ctx, q, draft.UserID, draft.PostID, draft.NicknameID, draft.CreatedAt); err != nil {
		return fmt.Errorf("save draft: %w", err)
	}

	return nil
}

func (s *Storage) Draft(ctx context.Context, userID int64) (comment.Draft, error) {
	const q = `SELECT user_id, post_id, nickname_id, created_at FROM comment_drafts WHERE user_id = $1`

	var d comment.Draft
	if err := s.pool.QueryRow(ctx, q, userID).Scan(&d.UserID, &d.PostID, &d.NicknameID, &d.CreatedAt); err != nil {
		return comment.Draft{}, notFound(err)
	}

	return d, nil
}

func (s *Storage) DeleteDraft(ctx context.Context, userID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM comment_drafts WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}

	return nil
}

func (s *Storage) CreateComment(ctx context.Context, c comment.Comment) (int64, error) {
	media, err := json.Marshal(nonNilMedia(c.Media))
	if err != nil {
		return 0, fmt.Errorf("encode media: %w", err)
	}

	const q = `
INSERT INTO comments (user_id, post_id, nickname_id, content_text, media_json, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id`

	var id int64
	err = s.pool.QueryRow(ctx, q, c.UserID, c.PostID, c.NicknameID, c.Text, media, c.Status, c.CreatedAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create comment: %w", err)
	}

	return id, nil
}

// MarkCommentPublished stores the group message id and, in the same transaction,
// the identity_map row that lets moderators trace the message back to its author.
func (s *Storage) MarkCommentPublished(ctx context.Context, id int64, messageID int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const update = `
UPDATE comments SET message_id_in_group = $2, status = $3
WHERE id = $1
RETURNING user_id, nickname_id`

	var userID, nicknameID int64
	if err := tx.QueryRow(ctx, update, id, messageID, comment.StatusPublished).Scan(&userID, &nicknameID); err != nil {
		return fmt.Errorf("mark comment %d published: %w", id, notFound(err))
	}

	const identity = `
INSERT INTO identity_map (message_id_in_group, comment_id, user_id, nickname_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (message_id_in_group) DO UPDATE
SET comment_id = EXCLUDED.comment_id,
    user_id = EXCLUDED.user_id,
    nickname_id = EXCLUDED.nickname_id`

	if _, err := tx.Exec(ctx, identity, messageID, id, userID, nicknameID); err != nil {
		return fmt.Errorf("record identity for comment %d: %w", id, err)
	}

	return tx.Commit(ctx)
}

func (s *Storage) MarkCommentFailed(ctx context.Context, id int64) error {
	const q = `UPDATE comments SET status = $2 WHERE id = $1`

	if _, err := s.pool.Exec(ctx, q, id, comment.StatusFailed); err != nil {
		return fmt.Errorf("mark comment %d failed: %w", id, err)
	}

	return nil
}

// nonNilMedia keeps the JSONB column an array rather than null.
func nonNilMedia(media []comment.Media) []comment.Media {
	if media == nil {
		return []comment.Media{}
	}

	return media
}

// SaveReport records a complaint from a reader; the moderation queue reads it later.
func (s *Storage) SaveReport(ctx context.Context, targetType string, targetID, reporterID int64, reason string) error {
	const q = `
INSERT INTO reports (target_type, target_id, reporter_user_id, reason)
VALUES ($1, $2, $3, $4)`

	if _, err := s.pool.Exec(ctx, q, targetType, targetID, reporterID, reason); err != nil {
		return fmt.Errorf("save report: %w", err)
	}

	return nil
}
