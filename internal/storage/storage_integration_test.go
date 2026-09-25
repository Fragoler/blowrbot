//go:build integration

// These tests run the real SQL against a real Postgres. They are behind a build
// tag so that `task test` stays offline; see `task test:integration`.
package storage_test

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/comment"
	"loudbot/internal/config"
	"loudbot/internal/storage"
)

// envDSN points at a throwaway database; everything in it is dropped and rebuilt.
const envDSN = "LOUDBOT_TEST_POSTGRES"

func dsn(t *testing.T) string {
	t.Helper()

	raw := os.Getenv(envDSN)
	if raw == "" {
		t.Skipf("%s is not set", envDSN)
	}

	return raw
}

// testConfig turns the test DSN back into the struct the production code takes,
// which incidentally checks that config.Postgres can express a real connection.
func testConfig(t *testing.T) config.Postgres {
	t.Helper()

	u, err := url.Parse(dsn(t))
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	password, _ := u.User.Password()

	return config.Postgres{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: password,
		DBName:   strings.TrimPrefix(u.Path, "/"),
		SSLMode:  u.Query().Get("sslmode"),
		MaxConns: 5,
	}
}

// probe is an independent connection used to assert on rows the repository wrote.
func probe(ctx context.Context, t *testing.T) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(ctx, dsn(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	return conn
}

// reset empties everything the tests write. Without it a test would read rows
// left by an earlier run.
const reset = `
TRUNCATE posts, comment_drafts, comments, suggested_posts, reports, identity_map,
         audit_log, users
RESTART IDENTITY CASCADE`

// Masks come from config.toml now, so the tests pick their own labels.
const (
	fox = "Лис"
	owl = "Сова"
)

func open(t *testing.T) (*storage.Storage, context.Context) {
	t.Helper()

	cfg := testConfig(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	require.NoError(t, storage.Migrate(ctx, cfg, log))

	_, err := probe(ctx, t).Exec(ctx, reset)
	require.NoError(t, err)

	st, err := storage.Open(ctx, cfg, log)
	require.NoError(t, err)
	t.Cleanup(st.Close)

	return st, ctx
}

func TestMigrateIsIdempotent(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	require.NoError(t, storage.Migrate(ctx, cfg, log))
	require.NoError(t, storage.Migrate(ctx, cfg, log), "a second run must be a no-op")
}

func TestUserLifecycle(t *testing.T) {
	st, ctx := open(t)

	const userID = int64(10_001)

	user, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, userID, user.ID)
	assert.False(t, user.Banned)
	assert.Empty(t, user.LastNickname, "a fresh user has no remembered mask")

	again, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, user, again, "EnsureUser is an upsert, not an insert")

	require.NoError(t, st.SetLastNickname(ctx, userID, fox))

	updated, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, fox, updated.LastNickname)
}

func TestPostLinking(t *testing.T) {
	st, ctx := open(t)

	post := comment.Post{
		ChannelMessageID:    4242,
		DiscussionChatID:    -100500,
		DiscussionMessageID: 77,
		Body:                "Текст поста",
		ChannelUsername:     "anon_channel",
	}
	require.NoError(t, st.LinkPost(ctx, post))

	// Telegram may deliver the auto-forward more than once.
	require.NoError(t, st.LinkPost(ctx, post))

	got, err := st.Post(ctx, post.ChannelMessageID)
	require.NoError(t, err)
	assert.Equal(t, post.DiscussionChatID, got.DiscussionChatID)
	assert.Equal(t, post.DiscussionMessageID, got.DiscussionMessageID)
	assert.Zero(t, got.InviteMessageID, "a NULL invite column must read back as zero")
	assert.Equal(t, "Текст поста", got.Body, "the body is what the bot quotes back to an author")
	assert.Equal(t, "anon_channel", got.ChannelUsername)
	assert.False(t, got.CreatedAt.IsZero())

	require.NoError(t, st.MarkInvitePosted(ctx, post.ChannelMessageID, 555))

	got, err = st.Post(ctx, post.ChannelMessageID)
	require.NoError(t, err)
	assert.Equal(t, 555, got.InviteMessageID)

	// A redelivered auto-forward re-links the post; the invite marker must survive.
	require.NoError(t, st.LinkPost(ctx, post))

	got, err = st.Post(ctx, post.ChannelMessageID)
	require.NoError(t, err)
	assert.Equal(t, 555, got.InviteMessageID, "re-linking must not clear the invite marker")

	_, err = st.Post(ctx, 999_999)
	require.ErrorIs(t, err, comment.ErrNotFound)
}

func TestDraftLifecycle(t *testing.T) {
	st, ctx := open(t)

	const userID = int64(10_002)

	_, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)

	_, err = st.Draft(ctx, userID)
	require.ErrorIs(t, err, comment.ErrNotFound)

	created := time.Now().UTC().Truncate(time.Millisecond)
	draft := comment.Draft{UserID: userID, PostID: 4242, CreatedAt: created}
	require.NoError(t, st.SaveDraft(ctx, draft))

	got, err := st.Draft(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, draft.PostID, got.PostID)
	assert.False(t, got.Staged(), "a fresh draft has nothing written yet")
	assert.Empty(t, got.Media, "a NULL-free media column reads back as an empty slice")
	assert.WithinDuration(t, created, got.CreatedAt, time.Millisecond)

	// Staging a message must overwrite in place: user_id is the primary key.
	draft.Body = "привет"
	draft.Media = []comment.Media{{Type: comment.MediaPhoto, FileID: "f1", FileUniqueID: "u1"}}
	draft.UserMessageID = 500
	draft.PromptMessageID = 501
	require.NoError(t, st.SaveDraft(ctx, draft))

	got, err = st.Draft(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, "привет", got.Body)
	assert.Equal(t, draft.Media, got.Media)
	assert.True(t, got.Staged())
	assert.Equal(t, 500, got.UserMessageID)
	assert.Equal(t, 501, got.PromptMessageID)

	require.NoError(t, st.DeleteDraft(ctx, userID))
	_, err = st.Draft(ctx, userID)
	require.ErrorIs(t, err, comment.ErrNotFound)

	require.NoError(t, st.DeleteDraft(ctx, userID), "deleting a missing draft is not an error")
}

func TestCommentPublishWritesIdentityMap(t *testing.T) {
	st, ctx := open(t)

	const (
		userID    = int64(10_003)
		messageID = 8801
	)

	_, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)

	id, err := st.CreateComment(ctx, comment.Comment{
		UserID:    userID,
		PostID:    4242,
		Nickname:  fox,
		Text:      "привет",
		Media:     []comment.Media{{Type: comment.MediaPhoto, FileID: "f1", FileUniqueID: "u1"}},
		Status:    comment.StatusPending,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NotZero(t, id)

	require.NoError(t, st.MarkCommentPublished(ctx, id, messageID))

	var (
		author     int64
		nickname   string
		commentID  int64
		status     string
		storedText string
	)

	err = probe(ctx, t).QueryRow(ctx, `
SELECT i.user_id, i.nickname, i.comment_id, c.status, c.content_text
FROM identity_map i JOIN comments c ON c.id = i.comment_id
WHERE i.message_id_in_group = $1`, messageID).
		Scan(&author, &nickname, &commentID, &status, &storedText)
	require.NoError(t, err)

	assert.Equal(t, userID, author, "identity_map is what lets a moderator trace an author")
	assert.Equal(t, fox, nickname, "the label is stored verbatim, not a foreign key")
	assert.Equal(t, id, commentID)
	assert.Equal(t, string(comment.StatusPublished), status)
	assert.Equal(t, "привет", storedText)

	// Telegram never reuses a message id, but a retry of the same publish must not break.
	require.NoError(t, st.MarkCommentPublished(ctx, id, messageID))
}

func TestCommentWithoutMedia(t *testing.T) {
	st, ctx := open(t)

	const userID = int64(10_004)

	_, err := st.EnsureUser(ctx, userID)
	require.NoError(t, err)

	// A nil slice must land as an empty JSON array, not NULL.
	id, err := st.CreateComment(ctx, comment.Comment{
		UserID:    userID,
		PostID:    4242,
		Nickname:  fox,
		Text:      "без медиа",
		Status:    comment.StatusPending,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	var media string
	require.NoError(t, probe(ctx, t).
		QueryRow(ctx, `SELECT media_json::text FROM comments WHERE id = $1`, id).Scan(&media))
	assert.Equal(t, "[]", media)

	require.NoError(t, st.MarkCommentFailed(ctx, id))

	var status string
	require.NoError(t, probe(ctx, t).
		QueryRow(ctx, `SELECT status FROM comments WHERE id = $1`, id).Scan(&status))
	assert.Equal(t, string(comment.StatusFailed), status)
}

func TestMarkPublishedMissingComment(t *testing.T) {
	st, ctx := open(t)

	err := st.MarkCommentPublished(ctx, 999_999, 1)
	require.ErrorIs(t, err, comment.ErrNotFound)
}

func TestSaveReport(t *testing.T) {
	st, ctx := open(t)

	require.NoError(t, st.SaveReport(ctx, "comment", 1, 10_005, ""))
	require.NoError(t, st.SaveReport(ctx, "comment", 1, 10_006, "спам"))

	var n int
	require.NoError(t, probe(ctx, t).
		QueryRow(ctx, `SELECT count(*) FROM reports WHERE target_type = 'comment' AND target_id = 1`).Scan(&n))
	assert.Equal(t, 2, n, "every complaint is kept, duplicates included")
}
