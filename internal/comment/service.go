package comment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultMaxTextLen = 3500
	defaultSeparator  = ": "
	defaultDraftTTL   = time.Hour
)

// Options are the tunables the service reads from config.
type Options struct {
	BotUsername string
	// Nicknames is the mask list from config.toml, in the order it is offered.
	Nicknames         []Nickname
	MaxTextLen        int
	NicknameSeparator string
	// DraftTTL bounds how long a deep-link tap stays valid, so that an old draft
	// cannot silently attach a new message to a stale post.
	DraftTTL time.Duration
}

func (o Options) withDefaults() Options {
	if o.MaxTextLen <= 0 {
		o.MaxTextLen = defaultMaxTextLen
	}

	if o.NicknameSeparator == "" {
		o.NicknameSeparator = defaultSeparator
	}

	if o.DraftTTL <= 0 {
		o.DraftTTL = defaultDraftTTL
	}

	return o
}

// Service is the core of the comment flow.
type Service struct {
	repo  Repository
	pub   Publisher
	guard Guard
	opts  Options
	now   func() time.Time
}

// New builds the service; guard may be nil, which means "allow everything".
func New(repo Repository, pub Publisher, guard Guard, opts Options) *Service {
	if guard == nil {
		guard = AllowAll{}
	}

	return &Service{
		repo:  repo,
		pub:   pub,
		guard: guard,
		opts:  opts.withDefaults(),
		now:   time.Now,
	}
}

// SetClock replaces the time source; used by tests around draft expiry.
func (s *Service) SetClock(now func() time.Time) {
	s.now = now
}

// DeepLink is the URL for the button under a channel post.
func (s *Service) DeepLink(channelMessageID int) string {
	return DeepLink(s.opts.BotUsername, channelMessageID)
}

// StartResult tells the Telegram layer what keyboard to draw after a deep-link tap.
type StartResult struct {
	PostID    int
	Nicknames []Nickname
	Selected  Nickname
}

// Start opens a draft from a /start payload and preselects the last used mask.
func (s *Service) Start(ctx context.Context, userID int64, payload string) (StartResult, error) {
	postID, err := ParseStartPayload(payload)
	if err != nil {
		return StartResult{}, err
	}

	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return StartResult{}, err
	}

	if _, err := s.post(ctx, postID); err != nil {
		return StartResult{}, err
	}

	nicknames, err := s.Nicknames()
	if err != nil {
		return StartResult{}, err
	}

	selected := preselect(nicknames, user.LastNickname)

	draft := Draft{
		UserID:    userID,
		PostID:    postID,
		Nickname:  selected.Label,
		CreatedAt: s.now(),
	}
	if err := s.repo.SaveDraft(ctx, draft); err != nil {
		return StartResult{}, fmt.Errorf("save draft: %w", err)
	}

	return StartResult{PostID: postID, Nicknames: nicknames, Selected: selected}, nil
}

// ChooseNickname switches the mask of the open draft.
func (s *Service) ChooseNickname(ctx context.Context, userID int64, label string) (Nickname, error) {
	draft, err := s.draft(ctx, userID)
	if err != nil {
		return Nickname{}, err
	}

	nickname, err := s.Nickname(label)
	if err != nil {
		return Nickname{}, err
	}

	draft.Nickname = nickname.Label
	if err := s.repo.SaveDraft(ctx, draft); err != nil {
		return Nickname{}, fmt.Errorf("save draft: %w", err)
	}

	return nickname, nil
}

// SubmitRequest is the message the author sent to the bot in private.
type SubmitRequest struct {
	UserID int64
	Text   string
	Media  []Media
}

// Submit validates the message, publishes it into the post's thread and records it.
// The comment row is created before publication so that the report button can carry
// its id; on a Telegram failure the row is marked failed rather than left pending.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (Comment, error) {
	if _, err := s.activeUser(ctx, req.UserID); err != nil {
		return Comment{}, err
	}

	draft, err := s.draft(ctx, req.UserID)
	if err != nil {
		return Comment{}, err
	}

	text, err := s.validateContent(req)
	if err != nil {
		return Comment{}, err
	}

	post, err := s.post(ctx, draft.PostID)
	if err != nil {
		return Comment{}, err
	}

	nickname, err := s.Nickname(draft.Nickname)
	if err != nil {
		return Comment{}, err
	}

	guardReq := GuardRequest{
		UserID:   req.UserID,
		PostID:   draft.PostID,
		Text:     text,
		HasMedia: len(req.Media) > 0,
	}
	if err := s.guard.Check(ctx, guardReq); err != nil {
		return Comment{}, err
	}

	c := Comment{
		UserID:    req.UserID,
		PostID:    draft.PostID,
		Nickname:  nickname.Label,
		Text:      text,
		Media:     req.Media,
		Status:    StatusPending,
		CreatedAt: s.now(),
	}

	id, err := s.repo.CreateComment(ctx, c)
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	c.ID = id

	published, err := s.pub.PublishComment(ctx, PublishRequest{
		CommentID:        id,
		ChatID:           post.DiscussionChatID,
		ReplyToMessageID: post.DiscussionMessageID,
		Text:             FormatBody(nickname, text, s.opts.NicknameSeparator),
		Media:            req.Media,
	})
	if err != nil {
		if markErr := s.repo.MarkCommentFailed(ctx, id); markErr != nil {
			err = errors.Join(err, markErr)
		}

		return Comment{}, fmt.Errorf("publish comment: %w", err)
	}

	if err := s.repo.MarkCommentPublished(ctx, id, published.MessageID); err != nil {
		return Comment{}, fmt.Errorf("mark comment published: %w", err)
	}

	if err := s.repo.SetLastNickname(ctx, req.UserID, nickname.Label); err != nil {
		return Comment{}, fmt.Errorf("set last nickname: %w", err)
	}

	if err := s.repo.DeleteDraft(ctx, req.UserID); err != nil {
		return Comment{}, fmt.Errorf("delete draft: %w", err)
	}

	c.MessageID = published.MessageID
	c.Status = StatusPublished

	return c, nil
}

func (s *Service) validateContent(req SubmitRequest) (string, error) {
	text := strings.TrimSpace(req.Text)

	if text == "" && len(req.Media) == 0 {
		return "", ErrEmptyComment
	}

	if n := utf8.RuneCountInString(text); n > s.opts.MaxTextLen {
		return "", fmt.Errorf("%w: %d > %d", ErrTextTooLong, n, s.opts.MaxTextLen)
	}

	return text, nil
}

func (s *Service) activeUser(ctx context.Context, userID int64) (User, error) {
	user, err := s.repo.EnsureUser(ctx, userID)
	if err != nil {
		return User{}, fmt.Errorf("ensure user: %w", err)
	}

	if user.Banned {
		return User{}, ErrBanned
	}

	return user, nil
}

// draft returns the open draft, dropping and reporting it once it is older than the TTL.
func (s *Service) draft(ctx context.Context, userID int64) (Draft, error) {
	draft, err := s.repo.Draft(ctx, userID)
	switch {
	case errors.Is(err, ErrNotFound):
		return Draft{}, ErrNoDraft
	case err != nil:
		return Draft{}, fmt.Errorf("draft: %w", err)
	}

	if s.now().Sub(draft.CreatedAt) > s.opts.DraftTTL {
		if err := s.repo.DeleteDraft(ctx, userID); err != nil {
			return Draft{}, fmt.Errorf("delete expired draft: %w", err)
		}

		return Draft{}, ErrDraftExpired
	}

	return draft, nil
}

func (s *Service) post(ctx context.Context, channelMessageID int) (Post, error) {
	post, err := s.repo.Post(ctx, channelMessageID)
	switch {
	case errors.Is(err, ErrNotFound):
		return Post{}, fmt.Errorf("%w: %d", ErrUnknownPost, channelMessageID)
	case err != nil:
		return Post{}, fmt.Errorf("post: %w", err)
	}

	if post.DiscussionMessageID == 0 {
		return Post{}, fmt.Errorf("%w: %d has no discussion thread", ErrUnknownPost, channelMessageID)
	}

	return post, nil
}

// Nickname resolves a label against the configured list. A label that is no
// longer there — dropped from the config while a draft was open — is refused.
func (s *Service) Nickname(label string) (Nickname, error) {
	for _, n := range s.opts.Nicknames {
		if n.Label == label {
			return n, nil
		}
	}

	return Nickname{}, fmt.Errorf("%w: %q", ErrNicknameUnavailable, label)
}

// Nicknames returns the masks currently offered to authors.
func (s *Service) Nicknames() ([]Nickname, error) {
	if len(s.opts.Nicknames) == 0 {
		return nil, ErrNoNicknames
	}

	return s.opts.Nicknames, nil
}

// preselect keeps the last used mask when it is still on the list.
func preselect(nicknames []Nickname, last string) Nickname {
	for _, n := range nicknames {
		if n.Label == last {
			return n
		}
	}

	return nicknames[0]
}

// OnPostPublished records the discussion-group anchor of a new channel post and
// leaves the invitation comment under it. Telegram redelivers auto-forwards, so
// the invitation is posted at most once per post.
func (s *Service) OnPostPublished(ctx context.Context, post Post) error {
	existing, err := s.repo.Post(ctx, post.ChannelMessageID)
	switch {
	case err == nil && existing.InviteMessageID != 0:
		return nil
	case err != nil && !errors.Is(err, ErrNotFound):
		return fmt.Errorf("post: %w", err)
	}

	if err := s.repo.LinkPost(ctx, post); err != nil {
		return fmt.Errorf("link post: %w", err)
	}

	invite, err := s.pub.PublishInvite(ctx, InviteRequest{
		ChatID:           post.DiscussionChatID,
		ReplyToMessageID: post.DiscussionMessageID,
		DeepLink:         s.DeepLink(post.ChannelMessageID),
	})
	if err != nil {
		// The post is linked, so comments still work through the button under the
		// post itself; only the invitation is missing.
		return fmt.Errorf("publish invite: %w", err)
	}

	if err := s.repo.MarkInvitePosted(ctx, post.ChannelMessageID, invite.MessageID); err != nil {
		return fmt.Errorf("mark invite posted: %w", err)
	}

	return nil
}
