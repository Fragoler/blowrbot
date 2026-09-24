// Package comment holds the core logic of anonymous comments: it turns a deep-link
// tap into a draft, resolves the nickname mask and publishes the result into the
// discussion group. It talks to Telegram and to the database only through the
// interfaces declared here, so every branch below is unit-testable.
package comment

import (
	"context"
	"strings"
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
	ID             int64
	Banned         bool
	LastNicknameID int64
}

// Nickname is one entry of the curated mask list.
type Nickname struct {
	ID     int64
	Label  string
	Emoji  string
	Active bool
}

// Display renders the mask as readers see it in the discussion group.
func (n Nickname) Display() string {
	label := strings.TrimSpace(n.Label)
	emoji := strings.TrimSpace(n.Emoji)

	switch {
	case emoji == "":
		return label
	case label == "":
		return emoji
	default:
		return emoji + " " + label
	}
}

// Post links a channel post to its auto-forwarded copy in the discussion group.
// Replying to that copy is what puts a message into the post's comment thread.
type Post struct {
	ChannelMessageID    int
	DiscussionChatID    int64
	DiscussionMessageID int
	// InviteMessageID is the bot's first comment under the post, the one carrying
	// the "comment anonymously" button. Zero means it has not been posted yet.
	InviteMessageID int
	CreatedAt       time.Time
}

// Draft is what a user is currently composing: which post, under which mask.
type Draft struct {
	UserID     int64
	PostID     int
	NicknameID int64
	CreatedAt  time.Time
}

type Media struct {
	Type         MediaType
	FileID       string
	FileUniqueID string
}

type Comment struct {
	ID         int64
	UserID     int64
	PostID     int
	NicknameID int64
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
	SetLastNickname(ctx context.Context, userID, nicknameID int64) error

	ActiveNicknames(ctx context.Context) ([]Nickname, error)
	Nickname(ctx context.Context, id int64) (Nickname, error)

	LinkPost(ctx context.Context, post Post) error
	Post(ctx context.Context, channelMessageID int) (Post, error)
	MarkInvitePosted(ctx context.Context, channelMessageID, inviteMessageID int) error

	SaveDraft(ctx context.Context, draft Draft) error
	Draft(ctx context.Context, userID int64) (Draft, error)
	DeleteDraft(ctx context.Context, userID int64) error

	CreateComment(ctx context.Context, c Comment) (int64, error)
	MarkCommentPublished(ctx context.Context, id int64, messageID int) error
	MarkCommentFailed(ctx context.Context, id int64) error
}

// PublishRequest is a ready-to-send comment; the text already carries the mask prefix.
type PublishRequest struct {
	// CommentID is embedded into the report button, so it must be known before sending.
	CommentID int64
	ChatID    int64
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
