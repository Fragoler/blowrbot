// Package comment holds the core logic of anonymous comments: it turns a deep-link
// tap into a draft, resolves the nickname mask and publishes the result into the
// discussion group. It talks to Telegram and to the database only through the
// interfaces declared here, so every branch below is unit-testable.
package comment

import (
	"context"
	"time"
)

// MediaType is the kind of attachment carried by a comment.
type MediaType string

const (
	MediaPhoto    MediaType = "photo"
	MediaVideo    MediaType = "video"
	MediaDocument MediaType = "document"
)

// Status is the lifecycle of a comment row.
type Status string

const (
	// StatusPending is written before the message reaches the discussion group.
	StatusPending Status = "pending"
	// StatusPublished means the message is live and MessageID is set.
	StatusPublished Status = "published"
	// StatusFailed means Telegram refused the publication.
	StatusFailed Status = "failed"
	// StatusDeleted is set by moderation.
	StatusDeleted Status = "deleted"
)

type User struct {
	ID     int64
	Banned bool
	// LastNickname is the label last worn by this user, or empty for a new one.
	LastNickname string
}

// Nickname is one entry of the curated mask list. The list lives in config.toml,
// not in the database: editing it is a config change and a restart, no migration.
// Label is the identity — it is what gets stored on a published comment, so a mask
// later dropped from the config does not rewrite messages already in the channel.
type Nickname struct {
	Label string
}

// Post links a channel post to its auto-forwarded copy in the discussion group.
// Replying to that copy is what puts a message into the post's comment thread.
type Post struct {
	ChannelMessageID    int
	DiscussionChatID    int64
	DiscussionMessageID int
	// Body is the post's own text or caption, kept for quoting it back to an
	// author: the Bot API offers no way to read a message by id afterwards.
	Body string
	// ChannelUsername is empty for a private channel, which needs a /c/ link.
	ChannelUsername string
	// InviteMessageID is the bot's first comment under the post, the one carrying
	// the "comment anonymously" button. Zero means it has not been posted yet.
	InviteMessageID int
	CreatedAt       time.Time
}

// Draft is an author's session with the bot: the post they opened and the message
// they have written but not yet signed. The mask is picked last, so it is not here.
type Draft struct {
	UserID int64
	PostID int
	// ReplyToCommentID is set when the author arrived through the "ответить" link
	// under a comment; zero means they are commenting on the post itself.
	ReplyToCommentID int64
	Body             string
	Media            []Media
	// UserMessageID is the author's staged message; zero means nothing is staged
	// and the bot is still waiting for them to write.
	UserMessageID int
	// PromptMessageID is the bot's mask keyboard. Cancel deletes both ids.
	PromptMessageID int
	CreatedAt       time.Time
}

// Staged reports whether a message is waiting for a mask to be picked.
func (d Draft) Staged() bool {
	return d.UserMessageID != 0
}

type Media struct {
	Type         MediaType
	FileID       string
	FileUniqueID string
}

type Comment struct {
	ID       int64
	UserID   int64
	PostID   int
	Nickname string
	// ReplyToCommentID is the comment this one answers, zero for a top-level one.
	ReplyToCommentID int64
	// MessageID is the id of the published message inside the discussion group.
	MessageID int
	Text      string
	Media     []Media
	Status    Status
	CreatedAt time.Time
}

// Repository persists everything the comment flow needs. Implementations return
// ErrNotFound (wrapped or bare) when a row is missing.
type Repository interface {
	EnsureUser(ctx context.Context, userID int64) (User, error)
	SetLastNickname(ctx context.Context, userID int64, nickname string) error

	LinkPost(ctx context.Context, post Post) error
	Post(ctx context.Context, channelMessageID int) (Post, error)
	MarkInvitePosted(ctx context.Context, channelMessageID, inviteMessageID int) error

	SaveDraft(ctx context.Context, draft Draft) error
	Draft(ctx context.Context, userID int64) (Draft, error)
	DeleteDraft(ctx context.Context, userID int64) error

	Comment(ctx context.Context, id int64) (Comment, error)
	CreateComment(ctx context.Context, c Comment) (int64, error)
	MarkCommentPublished(ctx context.Context, id int64, messageID int) error
	MarkCommentFailed(ctx context.Context, id int64) error
}

// PublishRequest is a ready-to-send comment; Text is already HTML with the mask
// as its first line, so the transport sends it with the HTML parse mode.
type PublishRequest struct {
	ChatID int64
	// ReplyToMessageID is the discussion-group copy of the channel post.
	ReplyToMessageID int
	Text             string
	Media            []Media
}

// InviteRequest is the bot's own first comment under a post: the one that offers
// readers the button. The wording and the button label belong to the transport.
type InviteRequest struct {
	ChatID int64
	// ReplyToMessageID is the discussion-group copy of the channel post.
	ReplyToMessageID int
	// DeepLink opens a comment draft for this post in private chat with the bot.
	DeepLink string
}

type PublishResult struct {
	MessageID int
}

// Publisher sends messages to Telegram.
type Publisher interface {
	PublishComment(ctx context.Context, req PublishRequest) (PublishResult, error)
	PublishInvite(ctx context.Context, req InviteRequest) (PublishResult, error)
}

// GuardRequest is what antiabuse inspects before anything is stored or sent.
type GuardRequest struct {
	UserID   int64
	PostID   int
	Text     string
	HasMedia bool
}

// Guard is the antiabuse hook: rate limit, stop words, dedup. It returns
// *RejectedError when the comment must not be published.
type Guard interface {
	Check(ctx context.Context, req GuardRequest) error
}

// AllowAll is the permissive Guard used until internal/antiabuse lands.
type AllowAll struct{}

func (AllowAll) Check(context.Context, GuardRequest) error { return nil }
