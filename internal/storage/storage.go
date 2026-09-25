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

	"loudbot/internal/achievement"
	"loudbot/internal/comment"
	"loudbot/internal/config"
	"loudbot/internal/profile"
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
RETURNING user_id, is_banned, COALESCE(last_nickname, '')`

	var u comment.User
	if err := s.pool.QueryRow(ctx, q, userID).Scan(&u.ID, &u.Banned, &u.LastNickname); err != nil {
		return comment.User{}, fmt.Errorf("ensure user %d: %w", userID, err)
	}

	return u, nil
}

func (s *Storage) SetLastNickname(ctx context.Context, userID int64, nickname string) error {
	const q = `UPDATE users SET last_nickname = $2 WHERE user_id = $1`

	if _, err := s.pool.Exec(ctx, q, userID, nickname); err != nil {
		return fmt.Errorf("set last nickname: %w", err)
	}

	return nil
}

// LinkPost records the discussion-group copy of a channel post. It is called from
// the auto-forward update, which may arrive more than once.
func (s *Storage) LinkPost(ctx context.Context, post comment.Post) error {
	const q = `
INSERT INTO posts (channel_message_id, discussion_chat_id, discussion_message_id, body, channel_username)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (channel_message_id) DO UPDATE
SET discussion_chat_id = EXCLUDED.discussion_chat_id,
    discussion_message_id = EXCLUDED.discussion_message_id,
    body = EXCLUDED.body,
    channel_username = EXCLUDED.channel_username`

	if _, err := s.pool.Exec(ctx, q,
		post.ChannelMessageID, post.DiscussionChatID, post.DiscussionMessageID,
		post.Body, post.ChannelUsername,
	); err != nil {
		return fmt.Errorf("link post %d: %w", post.ChannelMessageID, err)
	}

	return nil
}

func (s *Storage) Post(ctx context.Context, channelMessageID int) (comment.Post, error) {
	const q = `
SELECT channel_message_id, discussion_chat_id, discussion_message_id,
       COALESCE(invite_message_id, 0), body, channel_username, created_at
FROM posts WHERE channel_message_id = $1`

	var p comment.Post
	err := s.pool.QueryRow(ctx, q, channelMessageID).
		Scan(&p.ChannelMessageID, &p.DiscussionChatID, &p.DiscussionMessageID,
			&p.InviteMessageID, &p.Body, &p.ChannelUsername, &p.CreatedAt)
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
INSERT INTO comment_drafts
    (user_id, post_id, reply_to_comment_id, body, media_json, user_message_id, prompt_message_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (user_id) DO UPDATE
SET post_id = EXCLUDED.post_id,
    reply_to_comment_id = EXCLUDED.reply_to_comment_id,
    body = EXCLUDED.body,
    media_json = EXCLUDED.media_json,
    user_message_id = EXCLUDED.user_message_id,
    prompt_message_id = EXCLUDED.prompt_message_id,
    created_at = EXCLUDED.created_at`

	media, err := json.Marshal(nonNilMedia(draft.Media))
	if err != nil {
		return fmt.Errorf("encode draft media: %w", err)
	}

	if _, err := s.pool.Exec(ctx, q,
		draft.UserID, draft.PostID, nullableID(draft.ReplyToCommentID), draft.Body, media,
		draft.UserMessageID, draft.PromptMessageID, draft.CreatedAt,
	); err != nil {
		return fmt.Errorf("save draft: %w", err)
	}

	return nil
}

func (s *Storage) Draft(ctx context.Context, userID int64) (comment.Draft, error) {
	const q = `
SELECT user_id, post_id, COALESCE(reply_to_comment_id, 0), body, media_json,
       user_message_id, prompt_message_id, created_at
FROM comment_drafts WHERE user_id = $1`

	var (
		d     comment.Draft
		media []byte
	)

	err := s.pool.QueryRow(ctx, q, userID).
		Scan(&d.UserID, &d.PostID, &d.ReplyToCommentID, &d.Body, &media,
			&d.UserMessageID, &d.PromptMessageID, &d.CreatedAt)
	if err != nil {
		return comment.Draft{}, notFound(err)
	}

	if err := json.Unmarshal(media, &d.Media); err != nil {
		return comment.Draft{}, fmt.Errorf("decode draft media: %w", err)
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
INSERT INTO comments
    (user_id, post_id, nickname, reply_to_comment_id, content_text, media_json, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`

	var id int64
	err = s.pool.QueryRow(ctx, q,
		c.UserID, c.PostID, c.Nickname, nullableID(c.ReplyToCommentID),
		c.Text, media, c.Status, c.CreatedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create comment: %w", err)
	}

	return id, nil
}

// Comment loads one comment, which is how a reply finds the message it answers.
func (s *Storage) Comment(ctx context.Context, id int64) (comment.Comment, error) {
	const q = `
SELECT id, user_id, post_id, nickname, COALESCE(reply_to_comment_id, 0),
       COALESCE(message_id_in_group, 0), content_text, media_json, status, created_at
FROM comments WHERE id = $1`

	var (
		c     comment.Comment
		media []byte
	)

	err := s.pool.QueryRow(ctx, q, id).
		Scan(&c.ID, &c.UserID, &c.PostID, &c.Nickname, &c.ReplyToCommentID,
			&c.MessageID, &c.Text, &media, &c.Status, &c.CreatedAt)
	if err != nil {
		return comment.Comment{}, notFound(err)
	}

	if err := json.Unmarshal(media, &c.Media); err != nil {
		return comment.Comment{}, fmt.Errorf("decode comment media: %w", err)
	}

	return c, nil
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
RETURNING user_id, nickname`

	var (
		userID   int64
		nickname string
	)
	if err := tx.QueryRow(ctx, update, id, messageID, comment.StatusPublished).Scan(&userID, &nickname); err != nil {
		return fmt.Errorf("mark comment %d published: %w", id, notFound(err))
	}

	const identity = `
INSERT INTO identity_map (message_id_in_group, comment_id, user_id, nickname)
VALUES ($1, $2, $3, $4)
ON CONFLICT (message_id_in_group) DO UPDATE
SET comment_id = EXCLUDED.comment_id,
    user_id = EXCLUDED.user_id,
    nickname = EXCLUDED.nickname`

	if _, err := tx.Exec(ctx, identity, messageID, id, userID, nickname); err != nil {
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

// nullableID maps the zero "no such row" id onto a real SQL NULL, so that the
// foreign key holds and an absent parent is not stored as comment 0.
func nullableID(id int64) *int64 {
	if id == 0 {
		return nil
	}

	return &id
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

// Remember records message ids of a private chat for a later wipe. It is called
// on every message in that chat, so a repeat must not fail.
func (s *Storage) Remember(ctx context.Context, userID int64, messageIDs ...int) error {
	const q = `
INSERT INTO private_messages (user_id, message_id)
SELECT $1, unnest($2::int[])
ON CONFLICT DO NOTHING`

	if _, err := s.pool.Exec(ctx, q, userID, int32s(messageIDs)); err != nil {
		return fmt.Errorf("remember private messages: %w", err)
	}

	return nil
}

// Messages lists the private chat's remembered message ids, oldest first.
func (s *Storage) Messages(ctx context.Context, userID int64) ([]int, error) {
	const q = `SELECT message_id FROM private_messages WHERE user_id = $1 ORDER BY message_id`

	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list private messages: %w", err)
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan private message: %w", err)
		}
		out = append(out, id)
	}

	return out, rows.Err()
}

// Forget drops the rows of messages that are gone from the chat.
func (s *Storage) Forget(ctx context.Context, userID int64, messageIDs ...int) error {
	const q = `DELETE FROM private_messages WHERE user_id = $1 AND message_id = ANY($2::int[])`

	if _, err := s.pool.Exec(ctx, q, userID, int32s(messageIDs)); err != nil {
		return fmt.Errorf("forget private messages: %w", err)
	}

	return nil
}

// int32s converts to the width Postgres uses for int[].
func int32s(ids []int) []int32 {
	out := make([]int32, 0, len(ids))
	for _, id := range ids {
		out = append(out, int32(id)) //nolint:gosec // G115: Telegram message ids fit in int32.
	}

	return out
}

// Nicknames are the masks a user may wear: the public ones — those no achievement
// points at — plus any unlocked by an achievement they hold.
func (s *Storage) Nicknames(ctx context.Context, userID int64) ([]comment.Nickname, error) {
	const q = `
SELECT n.label
FROM nicknames n
WHERE n.is_active
  AND (
    NOT EXISTS (SELECT 1 FROM achievement_nicknames an WHERE an.nickname_id = n.id)
    OR EXISTS (
      SELECT 1
      FROM achievement_nicknames an
      JOIN user_achievements ua ON ua.achievement_id = an.achievement_id
      WHERE an.nickname_id = n.id AND ua.user_id = $1
    )
  )
ORDER BY n.id`

	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list nicknames: %w", err)
	}
	defer rows.Close()

	var out []comment.Nickname
	for rows.Next() {
		var n comment.Nickname
		if err := rows.Scan(&n.Label); err != nil {
			return nil, fmt.Errorf("scan nickname: %w", err)
		}
		out = append(out, n)
	}

	return out, rows.Err()
}

// Achievements lists what a user has been granted, oldest first.
func (s *Storage) Achievements(ctx context.Context, userID int64) ([]profile.Achievement, error) {
	const q = `
SELECT a.code, a.title, a.description
FROM user_achievements ua
JOIN achievements a ON a.id = ua.achievement_id
WHERE ua.user_id = $1
ORDER BY ua.granted_at, a.id`

	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list achievements: %w", err)
	}
	defer rows.Close()

	var out []profile.Achievement
	for rows.Next() {
		var a profile.Achievement
		if err := rows.Scan(&a.Code, &a.Title, &a.Description); err != nil {
			return nil, fmt.Errorf("scan achievement: %w", err)
		}
		out = append(out, a)
	}

	return out, rows.Err()
}

// Activity counts what a user has published, replies told apart from comments.
func (s *Storage) Activity(ctx context.Context, userID int64) (profile.Activity, error) {
	const q = `
SELECT
    count(*) FILTER (WHERE reply_to_comment_id IS NULL),
    count(*) FILTER (WHERE reply_to_comment_id IS NOT NULL)
FROM comments
WHERE user_id = $1 AND status = $2`

	var a profile.Activity
	if err := s.pool.QueryRow(ctx, q, userID, comment.StatusPublished).Scan(&a.Comments, &a.Replies); err != nil {
		return profile.Activity{}, fmt.Errorf("count activity: %w", err)
	}

	return a, nil
}

// ActiveRules reads the auto-grant rules in force. It runs on every published
// comment, so a rule edited in the database takes effect without a restart.
func (s *Storage) ActiveRules(ctx context.Context) ([]achievement.Rule, error) {
	const q = `
SELECT r.id, r.achievement_id, a.code, a.title, a.description,
       r.after_hour, r.before_hour,
       r.min_length, r.max_length, r.upper_only, COALESCE(r.pattern, ''),
       r.min_comments, r.min_replies, r.min_posts, r.min_achievements
FROM achievement_rules r
JOIN achievements a ON a.id = r.achievement_id
WHERE r.is_active
ORDER BY r.id`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list achievement rules: %w", err)
	}
	defer rows.Close()

	var out []achievement.Rule
	for rows.Next() {
		var r achievement.Rule
		err := rows.Scan(
			&r.ID, &r.AchievementID, &r.AchievementCode, &r.AchievementTitle, &r.AchievementDescription,
			&r.AfterHour, &r.BeforeHour,
			&r.MinLength, &r.MaxLength, &r.UpperOnly, &r.Pattern,
			&r.MinComments, &r.MinReplies, &r.MinPosts, &r.MinAchievements,
		)
		if err != nil {
			return nil, fmt.Errorf("scan achievement rule: %w", err)
		}
		out = append(out, r)
	}

	return out, rows.Err()
}

// Counters are the totals the rules compare against. Posts counts approved
// suggestions, which stays zero until that flow exists.
func (s *Storage) Counters(ctx context.Context, userID int64) (achievement.Counters, error) {
	const q = `
SELECT
    (SELECT count(*) FROM comments
      WHERE user_id = $1 AND status = $2 AND reply_to_comment_id IS NULL),
    (SELECT count(*) FROM comments
      WHERE user_id = $1 AND status = $2 AND reply_to_comment_id IS NOT NULL),
    (SELECT count(*) FROM suggested_posts WHERE user_id = $1 AND status = 'approved'),
    (SELECT count(*) FROM user_achievements WHERE user_id = $1)`

	var c achievement.Counters
	err := s.pool.QueryRow(ctx, q, userID, comment.StatusPublished).
		Scan(&c.Comments, &c.Replies, &c.Posts, &c.Achievements)
	if err != nil {
		return achievement.Counters{}, fmt.Errorf("count achievement inputs: %w", err)
	}

	return c, nil
}

// Grant records an achievement and reports whether it was new; a rule that keeps
// matching must not hand out the same reward twice.
func (s *Storage) Grant(ctx context.Context, userID, achievementID int64) (bool, error) {
	const q = `
INSERT INTO user_achievements (user_id, achievement_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING`

	tag, err := s.pool.Exec(ctx, q, userID, achievementID)
	if err != nil {
		return false, fmt.Errorf("grant achievement %d: %w", achievementID, err)
	}

	return tag.RowsAffected() > 0, nil
}
