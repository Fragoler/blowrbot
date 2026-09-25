package comment_test

import (
	"context"
	"errors"
	"sync"

	"loudbot/internal/comment"
)

var errBoom = errors.New("boom")

// fakeRepo is an in-memory Repository. Any field ending in Err forces that method
// to fail, which is how the error branches are exercised.
type fakeRepo struct {
	mu sync.Mutex

	users    map[int64]comment.User
	posts    map[int]comment.Post
	drafts   map[int64]comment.Draft
	comments map[int64]comment.Comment

	nextCommentID int64

	ensureUserErr  error
	postErr        error
	linkPostErr    error
	markInviteErr  error
	draftErr       error
	saveDraftErr   error
	deleteDraftErr error
	createErr      error
	commentErr     error
	publishedErr   error
	failedErr      error
	lastNickErr    error

	calls []string
}

func newRepo() *fakeRepo {
	return &fakeRepo{
		users:    map[int64]comment.User{},
		posts:    map[int]comment.Post{},
		drafts:   map[int64]comment.Draft{},
		comments: map[int64]comment.Comment{},
	}
}

func (r *fakeRepo) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, name)
}

func (r *fakeRepo) withUser(u comment.User) *fakeRepo {
	r.users[u.ID] = u

	return r
}

func (r *fakeRepo) withPost(p comment.Post) *fakeRepo {
	r.posts[p.ChannelMessageID] = p

	return r
}

func (r *fakeRepo) withDraft(d comment.Draft) *fakeRepo {
	r.drafts[d.UserID] = d

	return r
}

func (r *fakeRepo) EnsureUser(_ context.Context, userID int64) (comment.User, error) {
	r.record("EnsureUser")

	if r.ensureUserErr != nil {
		return comment.User{}, r.ensureUserErr
	}

	user, ok := r.users[userID]
	if !ok {
		user = comment.User{ID: userID}
		r.users[userID] = user
	}

	return user, nil
}

func (r *fakeRepo) SetLastNickname(_ context.Context, userID int64, nickname string) error {
	r.record("SetLastNickname")

	if r.lastNickErr != nil {
		return r.lastNickErr
	}

	user := r.users[userID]
	user.ID = userID
	user.LastNickname = nickname
	r.users[userID] = user

	return nil
}

func (r *fakeRepo) LinkPost(_ context.Context, post comment.Post) error {
	r.record("LinkPost")

	if r.linkPostErr != nil {
		return r.linkPostErr
	}

	// Keep an invite that a previous call already recorded.
	if existing, ok := r.posts[post.ChannelMessageID]; ok && post.InviteMessageID == 0 {
		post.InviteMessageID = existing.InviteMessageID
	}
	r.posts[post.ChannelMessageID] = post

	return nil
}

func (r *fakeRepo) MarkInvitePosted(_ context.Context, channelMessageID, inviteMessageID int) error {
	r.record("MarkInvitePosted")

	if r.markInviteErr != nil {
		return r.markInviteErr
	}

	post := r.posts[channelMessageID]
	post.InviteMessageID = inviteMessageID
	r.posts[channelMessageID] = post

	return nil
}

func (r *fakeRepo) Post(_ context.Context, channelMessageID int) (comment.Post, error) {
	r.record("Post")

	if r.postErr != nil {
		return comment.Post{}, r.postErr
	}

	p, ok := r.posts[channelMessageID]
	if !ok {
		return comment.Post{}, comment.ErrNotFound
	}

	return p, nil
}

func (r *fakeRepo) SaveDraft(_ context.Context, draft comment.Draft) error {
	r.record("SaveDraft")

	if r.saveDraftErr != nil {
		return r.saveDraftErr
	}

	r.drafts[draft.UserID] = draft

	return nil
}

func (r *fakeRepo) Draft(_ context.Context, userID int64) (comment.Draft, error) {
	r.record("Draft")

	if r.draftErr != nil {
		return comment.Draft{}, r.draftErr
	}

	d, ok := r.drafts[userID]
	if !ok {
		return comment.Draft{}, comment.ErrNotFound
	}

	return d, nil
}

func (r *fakeRepo) DeleteDraft(_ context.Context, userID int64) error {
	r.record("DeleteDraft")

	if r.deleteDraftErr != nil {
		return r.deleteDraftErr
	}

	delete(r.drafts, userID)

	return nil
}

func (r *fakeRepo) Comment(_ context.Context, id int64) (comment.Comment, error) {
	r.record("Comment")

	if r.commentErr != nil {
		return comment.Comment{}, r.commentErr
	}

	c, ok := r.comments[id]
	if !ok {
		return comment.Comment{}, comment.ErrNotFound
	}

	return c, nil
}

func (r *fakeRepo) withComment(c comment.Comment) *fakeRepo {
	r.comments[c.ID] = c
	if c.ID > r.nextCommentID {
		r.nextCommentID = c.ID
	}

	return r
}

func (r *fakeRepo) CreateComment(_ context.Context, c comment.Comment) (int64, error) {
	r.record("CreateComment")

	if r.createErr != nil {
		return 0, r.createErr
	}

	r.nextCommentID++
	c.ID = r.nextCommentID
	r.comments[c.ID] = c

	return c.ID, nil
}

func (r *fakeRepo) MarkCommentPublished(_ context.Context, id int64, messageID int) error {
	r.record("MarkCommentPublished")

	if r.publishedErr != nil {
		return r.publishedErr
	}

	c := r.comments[id]
	c.MessageID = messageID
	c.Status = comment.StatusPublished
	r.comments[id] = c

	return nil
}

func (r *fakeRepo) MarkCommentFailed(_ context.Context, id int64) error {
	r.record("MarkCommentFailed")

	if r.failedErr != nil {
		return r.failedErr
	}

	c := r.comments[id]
	c.Status = comment.StatusFailed
	r.comments[id] = c

	return nil
}

// fakePublisher records what would have been sent to Telegram.
type fakePublisher struct {
	requests []comment.PublishRequest
	result   comment.PublishResult
	err      error

	invites      []comment.InviteRequest
	inviteResult comment.PublishResult
	inviteErr    error
}

func (p *fakePublisher) PublishInvite(_ context.Context, req comment.InviteRequest) (comment.PublishResult, error) {
	p.invites = append(p.invites, req)

	if p.inviteErr != nil {
		return comment.PublishResult{}, p.inviteErr
	}

	return p.inviteResult, nil
}

func (p *fakePublisher) PublishComment(_ context.Context, req comment.PublishRequest) (comment.PublishResult, error) {
	p.requests = append(p.requests, req)

	if p.err != nil {
		return comment.PublishResult{}, p.err
	}

	return p.result, nil
}

// blockingGuard rejects everything with a fixed reason.
type blockingGuard struct {
	reason   string
	requests []comment.GuardRequest
}

func (g *blockingGuard) Check(_ context.Context, req comment.GuardRequest) error {
	g.requests = append(g.requests, req)

	return &comment.RejectedError{Reason: g.reason}
}

// recordingGuard allows everything but remembers what it saw.
type recordingGuard struct {
	requests []comment.GuardRequest
}

func (g *recordingGuard) Check(_ context.Context, req comment.GuardRequest) error {
	g.requests = append(g.requests, req)

	return nil
}
