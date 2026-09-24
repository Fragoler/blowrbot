package comment_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/comment"
)

const (
	userID       = int64(777)
	postID       = 42
	discussionID = int64(-100500)
	threadMsgID  = 9001
)

var (
	fox = comment.Nickname{ID: 1, Label: "Лис", Emoji: "🦊", Active: true}
	owl = comment.Nickname{ID: 2, Label: "Сова", Emoji: "🦉", Active: true}

	now = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
)

func testOptions() comment.Options {
	return comment.Options{
		BotUsername:       "anon_bot",
		MaxTextLen:        10,
		NicknameSeparator: ": ",
		DraftTTL:          time.Hour,
	}
}

func newService(t *testing.T, repo *fakeRepo, pub *fakePublisher, guard comment.Guard) *comment.Service {
	t.Helper()

	svc := comment.New(repo, pub, guard, testOptions())
	svc.SetClock(func() time.Time { return now })

	return svc
}

// fullRepo is the happy-path world: an active user, two masks and a post with a thread.
func fullRepo() *fakeRepo {
	return newRepo().
		withUser(comment.User{ID: userID}).
		withNickname(fox).
		withNickname(owl).
		withPost(comment.Post{
			ChannelMessageID:    postID,
			DiscussionChatID:    discussionID,
			DiscussionMessageID: threadMsgID,
		})
}

func TestStartOpensDraft(t *testing.T) {
	t.Parallel()

	repo := fullRepo()
	svc := newService(t, repo, &fakePublisher{}, nil)

	got, err := svc.Start(context.Background(), userID, "comment_42")
	require.NoError(t, err)

	assert.Equal(t, postID, got.PostID)
	assert.Equal(t, []comment.Nickname{fox, owl}, got.Nicknames)
	assert.Equal(t, fox, got.Selected, "first active mask is preselected for a new user")

	assert.Equal(t, comment.Draft{
		UserID:     userID,
		PostID:     postID,
		NicknameID: fox.ID,
		CreatedAt:  now,
	}, repo.drafts[userID])
}

func TestStartPreselectsLastNickname(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withUser(comment.User{ID: userID, LastNicknameID: owl.ID})
	svc := newService(t, repo, &fakePublisher{}, nil)

	got, err := svc.Start(context.Background(), userID, "comment_42")
	require.NoError(t, err)
	assert.Equal(t, owl, got.Selected)
}

func TestStartFallsBackWhenLastNicknameIsGone(t *testing.T) {
	t.Parallel()

	// The mask the user last wore was disabled by an admin.
	repo := fullRepo().withUser(comment.User{ID: userID, LastNicknameID: 99})
	svc := newService(t, repo, &fakePublisher{}, nil)

	got, err := svc.Start(context.Background(), userID, "comment_42")
	require.NoError(t, err)
	assert.Equal(t, fox, got.Selected)
}

func TestStartOmitsInactiveNicknames(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withNickname(comment.Nickname{ID: 3, Label: "Ёж", Active: false})
	svc := newService(t, repo, &fakePublisher{}, nil)

	got, err := svc.Start(context.Background(), userID, "comment_42")
	require.NoError(t, err)
	assert.Equal(t, []comment.Nickname{fox, owl}, got.Nicknames)
}

func TestStartErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		repo    func() *fakeRepo
		payload string
		wantErr error
	}{
		{
			name:    "malformed payload",
			repo:    fullRepo,
			payload: "hello",
			wantErr: comment.ErrBadPayload,
		},
		{
			name:    "banned user",
			repo:    func() *fakeRepo { return fullRepo().withUser(comment.User{ID: userID, Banned: true}) },
			payload: "comment_42",
			wantErr: comment.ErrBanned,
		},
		{
			name:    "unknown post",
			repo:    fullRepo,
			payload: "comment_43",
			wantErr: comment.ErrUnknownPost,
		},
		{
			name: "post without discussion thread",
			repo: func() *fakeRepo {
				return fullRepo().withPost(comment.Post{ChannelMessageID: postID, DiscussionChatID: discussionID})
			},
			payload: "comment_42",
			wantErr: comment.ErrUnknownPost,
		},
		{
			name: "no active nicknames",
			repo: func() *fakeRepo {
				return newRepo().
					withUser(comment.User{ID: userID}).
					withPost(comment.Post{ChannelMessageID: postID, DiscussionChatID: discussionID, DiscussionMessageID: threadMsgID})
			},
			payload: "comment_42",
			wantErr: comment.ErrNoNicknames,
		},
		{
			name:    "repository failure",
			repo:    func() *fakeRepo { r := fullRepo(); r.nicknamesErr = errBoom; return r },
			payload: "comment_42",
			wantErr: errBoom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := tc.repo()
			svc := newService(t, repo, &fakePublisher{}, nil)

			_, err := svc.Start(context.Background(), userID, tc.payload)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Empty(t, repo.drafts, "a failed start must not leave a draft behind")
		})
	}
}

func TestChooseNickname(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	svc := newService(t, repo, &fakePublisher{}, nil)

	got, err := svc.ChooseNickname(context.Background(), userID, owl.ID)
	require.NoError(t, err)
	assert.Equal(t, owl, got)
	assert.Equal(t, owl.ID, repo.drafts[userID].NicknameID)
	assert.Equal(t, postID, repo.drafts[userID].PostID, "switching the mask keeps the draft's post")
}

func TestChooseNicknameErrors(t *testing.T) {
	t.Parallel()

	openDraft := comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now}

	cases := []struct {
		name       string
		repo       func() *fakeRepo
		nicknameID int64
		wantErr    error
	}{
		{
			name:       "no draft",
			repo:       fullRepo,
			nicknameID: owl.ID,
			wantErr:    comment.ErrNoDraft,
		},
		{
			name: "expired draft",
			repo: func() *fakeRepo {
				d := openDraft
				d.CreatedAt = now.Add(-2 * time.Hour)
				return fullRepo().withDraft(d)
			},
			nicknameID: owl.ID,
			wantErr:    comment.ErrDraftExpired,
		},
		{
			name:       "unknown nickname",
			repo:       func() *fakeRepo { return fullRepo().withDraft(openDraft) },
			nicknameID: 404,
			wantErr:    comment.ErrNicknameUnavailable,
		},
		{
			name: "disabled nickname",
			repo: func() *fakeRepo {
				return fullRepo().
					withDraft(openDraft).
					withNickname(comment.Nickname{ID: 3, Label: "Ёж", Active: false})
			},
			nicknameID: 3,
			wantErr:    comment.ErrNicknameUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, tc.repo(), &fakePublisher{}, nil)

			_, err := svc.ChooseNickname(context.Background(), userID, tc.nicknameID)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestExpiredDraftIsDropped(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{
		UserID:     userID,
		PostID:     postID,
		NicknameID: fox.ID,
		CreatedAt:  now.Add(-2 * time.Hour),
	})
	svc := newService(t, repo, &fakePublisher{}, nil)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: "hi"})
	require.ErrorIs(t, err, comment.ErrDraftExpired)
	assert.Empty(t, repo.drafts, "an expired draft is removed so the next tap starts clean")
}

func TestDraftAtExactTTLIsStillValid(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{
		UserID:     userID,
		PostID:     postID,
		NicknameID: fox.ID,
		CreatedAt:  now.Add(-time.Hour),
	})
	svc := newService(t, repo, &fakePublisher{result: comment.PublishResult{MessageID: 1}}, nil)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: "hi"})
	require.NoError(t, err)
}

func TestSubmitPublishesAndRecords(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: owl.ID, CreatedAt: now})
	pub := &fakePublisher{result: comment.PublishResult{MessageID: 555}}
	guard := &recordingGuard{}
	svc := newService(t, repo, pub, guard)

	media := []comment.Media{{Type: comment.MediaPhoto, FileID: "f1", FileUniqueID: "u1"}}

	got, err := svc.Submit(context.Background(), comment.SubmitRequest{
		UserID: userID,
		Text:   "  привет  ",
		Media:  media,
	})
	require.NoError(t, err)

	assert.Equal(t, comment.StatusPublished, got.Status)
	assert.Equal(t, 555, got.MessageID)
	assert.Equal(t, "привет", got.Text, "the stored text is trimmed and carries no mask prefix")
	assert.Equal(t, owl.ID, got.NicknameID)
	assert.Equal(t, postID, got.PostID)
	assert.NotZero(t, got.ID)

	require.Len(t, pub.requests, 1)
	assert.Equal(t, comment.PublishRequest{
		CommentID:        got.ID,
		ChatID:           discussionID,
		ReplyToMessageID: threadMsgID,
		Text:             "🦉 Сова: привет",
		Media:            media,
	}, pub.requests[0], "the comment lands in the post's thread with the mask prefix")

	require.Len(t, guard.requests, 1)
	assert.Equal(t, comment.GuardRequest{
		UserID:   userID,
		PostID:   postID,
		Text:     "привет",
		HasMedia: true,
	}, guard.requests[0])

	stored := repo.comments[got.ID]
	assert.Equal(t, comment.StatusPublished, stored.Status)
	assert.Equal(t, 555, stored.MessageID)

	assert.Equal(t, owl.ID, repo.users[userID].LastNicknameID, "the mask is remembered for the next comment")
	assert.Empty(t, repo.drafts, "a published comment closes the draft")
}

func TestSubmitMediaOnly(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	pub := &fakePublisher{result: comment.PublishResult{MessageID: 1}}
	svc := newService(t, repo, pub, nil)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{
		UserID: userID,
		Media:  []comment.Media{{Type: comment.MediaVideo, FileID: "v1"}},
	})
	require.NoError(t, err)

	require.Len(t, pub.requests, 1)
	assert.Equal(t, "🦊 Лис", pub.requests[0].Text, "a media-only comment is captioned with the mask alone")
}

func TestSubmitCountsRunesNotBytes(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	pub := &fakePublisher{result: comment.PublishResult{MessageID: 1}}
	svc := newService(t, repo, pub, nil)

	// MaxTextLen is 10: ten Cyrillic runes are 20 bytes and must still pass.
	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: strings.Repeat("я", 10)})
	require.NoError(t, err)

	repo.withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})

	_, err = svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: strings.Repeat("я", 11)})
	require.ErrorIs(t, err, comment.ErrTextTooLong)
}

func TestSubmitRejectedByGuard(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	pub := &fakePublisher{}
	guard := &blockingGuard{reason: "слишком часто"}
	svc := newService(t, repo, pub, guard)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: "hi"})

	rejected, ok := comment.Rejected(err)
	require.True(t, ok, "the caller must be able to show the reason to the author")
	assert.Equal(t, "слишком часто", rejected.Reason)

	assert.Empty(t, pub.requests, "a rejected comment never reaches Telegram")
	assert.Empty(t, repo.comments, "a rejected comment is not stored")
	assert.NotEmpty(t, repo.drafts, "the draft survives so the author can retry")
}

func TestSubmitMarksCommentFailedWhenTelegramRefuses(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	pub := &fakePublisher{err: errBoom}
	svc := newService(t, repo, pub, nil)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: "hi"})
	require.ErrorIs(t, err, errBoom)

	require.Len(t, repo.comments, 1)
	for _, c := range repo.comments {
		assert.Equal(t, comment.StatusFailed, c.Status, "a pending row must not be left behind")
	}

	assert.NotEmpty(t, repo.drafts, "the draft survives a transport failure so the author can retry")
}

func TestSubmitErrors(t *testing.T) {
	t.Parallel()

	openDraft := comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now}

	cases := []struct {
		name    string
		repo    func() *fakeRepo
		req     comment.SubmitRequest
		wantErr error
	}{
		{
			name: "banned user",
			repo: func() *fakeRepo {
				return fullRepo().withUser(comment.User{ID: userID, Banned: true}).withDraft(openDraft)
			},
			req:     comment.SubmitRequest{UserID: userID, Text: "hi"},
			wantErr: comment.ErrBanned,
		},
		{
			name:    "no draft",
			repo:    fullRepo,
			req:     comment.SubmitRequest{UserID: userID, Text: "hi"},
			wantErr: comment.ErrNoDraft,
		},
		{
			name:    "empty comment",
			repo:    func() *fakeRepo { return fullRepo().withDraft(openDraft) },
			req:     comment.SubmitRequest{UserID: userID, Text: "   "},
			wantErr: comment.ErrEmptyComment,
		},
		{
			name:    "text too long",
			repo:    func() *fakeRepo { return fullRepo().withDraft(openDraft) },
			req:     comment.SubmitRequest{UserID: userID, Text: strings.Repeat("a", 11)},
			wantErr: comment.ErrTextTooLong,
		},
		{
			name: "post deleted while composing",
			repo: func() *fakeRepo {
				r := newRepo().withUser(comment.User{ID: userID}).withNickname(fox).withDraft(openDraft)

				return r
			},
			req:     comment.SubmitRequest{UserID: userID, Text: "hi"},
			wantErr: comment.ErrUnknownPost,
		},
		{
			name: "nickname disabled while composing",
			repo: func() *fakeRepo {
				r := fullRepo().withDraft(openDraft)
				r.nicknames[fox.ID] = comment.Nickname{ID: fox.ID, Label: fox.Label, Active: false}

				return r
			},
			req:     comment.SubmitRequest{UserID: userID, Text: "hi"},
			wantErr: comment.ErrNicknameUnavailable,
		},
		{
			name:    "create comment fails",
			repo:    func() *fakeRepo { r := fullRepo().withDraft(openDraft); r.createErr = errBoom; return r },
			req:     comment.SubmitRequest{UserID: userID, Text: "hi"},
			wantErr: errBoom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := tc.repo()
			pub := &fakePublisher{result: comment.PublishResult{MessageID: 1}}
			svc := newService(t, repo, pub, nil)

			_, err := svc.Submit(context.Background(), tc.req)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Empty(t, pub.requests, "nothing is published when validation fails")
		})
	}
}

func TestSubmitValidatesBeforeTouchingTelegram(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	guard := &recordingGuard{}
	pub := &fakePublisher{}
	svc := newService(t, repo, pub, guard)

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: ""})
	require.ErrorIs(t, err, comment.ErrEmptyComment)
	assert.Empty(t, guard.requests, "antiabuse is not consulted for an empty message")
}

func TestServiceDeepLinkUsesConfiguredUsername(t *testing.T) {
	t.Parallel()

	svc := newService(t, fullRepo(), &fakePublisher{}, nil)
	assert.Equal(t, "https://t.me/anon_bot?start=comment_42", svc.DeepLink(postID))
}

func TestDefaultsAreApplied(t *testing.T) {
	t.Parallel()

	repo := fullRepo().withDraft(comment.Draft{UserID: userID, PostID: postID, NicknameID: fox.ID, CreatedAt: now})
	pub := &fakePublisher{result: comment.PublishResult{MessageID: 1}}

	// Zero Options must not reject every comment via a zero-length limit.
	svc := comment.New(repo, pub, nil, comment.Options{BotUsername: "anon_bot"})
	svc.SetClock(func() time.Time { return now })

	_, err := svc.Submit(context.Background(), comment.SubmitRequest{UserID: userID, Text: "hi"})
	require.NoError(t, err)
	assert.Equal(t, "🦊 Лис: hi", pub.requests[0].Text)
}

func TestNicknames(t *testing.T) {
	t.Parallel()

	svc := newService(t, fullRepo(), &fakePublisher{}, nil)

	got, err := svc.Nicknames(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []comment.Nickname{fox, owl}, got)

	empty := newService(t, newRepo(), &fakePublisher{}, nil)
	_, err = empty.Nicknames(context.Background())
	require.ErrorIs(t, err, comment.ErrNoNicknames)
}

func TestNicknameByID(t *testing.T) {
	t.Parallel()

	svc := newService(t, fullRepo(), &fakePublisher{}, nil)

	got, err := svc.NicknameByID(context.Background(), owl.ID)
	require.NoError(t, err)
	assert.Equal(t, owl, got)

	_, err = svc.NicknameByID(context.Background(), 404)
	require.ErrorIs(t, err, comment.ErrNicknameUnavailable)
}

func newPost() comment.Post {
	return comment.Post{
		ChannelMessageID:    postID,
		DiscussionChatID:    discussionID,
		DiscussionMessageID: threadMsgID,
	}
}

func TestOnPostPublishedLinksAndInvites(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	pub := &fakePublisher{inviteResult: comment.PublishResult{MessageID: 4321}}
	svc := newService(t, repo, pub, nil)

	require.NoError(t, svc.OnPostPublished(context.Background(), newPost()))

	require.Len(t, pub.invites, 1)
	assert.Equal(t, comment.InviteRequest{
		ChatID:           discussionID,
		ReplyToMessageID: threadMsgID,
		DeepLink:         "https://t.me/anon_bot?start=comment_42",
	}, pub.invites[0], "the invitation replies to the forwarded post so it lands in the thread")

	stored := repo.posts[postID]
	assert.Equal(t, threadMsgID, stored.DiscussionMessageID)
	assert.Equal(t, 4321, stored.InviteMessageID, "the invite id is what makes a redelivery a no-op")
}

func TestOnPostPublishedIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	pub := &fakePublisher{inviteResult: comment.PublishResult{MessageID: 4321}}
	svc := newService(t, repo, pub, nil)

	ctx := context.Background()
	require.NoError(t, svc.OnPostPublished(ctx, newPost()))
	require.NoError(t, svc.OnPostPublished(ctx, newPost()), "Telegram redelivers auto-forwards")

	assert.Len(t, pub.invites, 1, "readers must not see two invitations under one post")
}

func TestOnPostPublishedRetriesInviteAfterFailure(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	pub := &fakePublisher{inviteErr: errBoom}
	svc := newService(t, repo, pub, nil)

	ctx := context.Background()
	err := svc.OnPostPublished(ctx, newPost())
	require.ErrorIs(t, err, errBoom)

	// The post is linked even though the invitation failed, so comments still work
	// through the button under the post itself.
	assert.Equal(t, threadMsgID, repo.posts[postID].DiscussionMessageID)
	assert.Zero(t, repo.posts[postID].InviteMessageID)

	// A later redelivery gets another chance at the invitation.
	pub.inviteErr = nil
	pub.inviteResult = comment.PublishResult{MessageID: 4321}

	require.NoError(t, svc.OnPostPublished(ctx, newPost()))
	assert.Len(t, pub.invites, 2)
	assert.Equal(t, 4321, repo.posts[postID].InviteMessageID)
}

func TestOnPostPublishedRepositoryErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		repo func() *fakeRepo
	}{
		{name: "lookup fails", repo: func() *fakeRepo { r := newRepo(); r.postErr = errBoom; return r }},
		{name: "link fails", repo: func() *fakeRepo { r := newRepo(); r.linkPostErr = errBoom; return r }},
		{name: "mark invite fails", repo: func() *fakeRepo { r := newRepo(); r.markInviteErr = errBoom; return r }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pub := &fakePublisher{inviteResult: comment.PublishResult{MessageID: 1}}
			svc := newService(t, tc.repo(), pub, nil)

			require.ErrorIs(t, svc.OnPostPublished(context.Background(), newPost()), errBoom)
		})
	}
}

func TestOnPostPublishedUpdatesMovedThread(t *testing.T) {
	t.Parallel()

	// A post already linked but never announced: the anchor is refreshed and the
	// invitation is posted on this pass.
	repo := newRepo().withPost(comment.Post{
		ChannelMessageID:    postID,
		DiscussionChatID:    discussionID,
		DiscussionMessageID: 1,
	})
	pub := &fakePublisher{inviteResult: comment.PublishResult{MessageID: 4321}}
	svc := newService(t, repo, pub, nil)

	require.NoError(t, svc.OnPostPublished(context.Background(), newPost()))

	assert.Equal(t, threadMsgID, repo.posts[postID].DiscussionMessageID)
	require.Len(t, pub.invites, 1)
	assert.Equal(t, threadMsgID, pub.invites[0].ReplyToMessageID)
}
