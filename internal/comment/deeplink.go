package comment

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

const (
	// startPrefix marks a /start payload that opens a comment draft.
	startPrefix = "comment_"
	// nicknamePrefix namespaces the inline callback data of a mask button.
	nicknamePrefix = "nick:"
	// CancelCallback is the callback data of the button that drops a staged message.
	CancelCallback = "cancel"

	// maxPayloadLen is the Telegram limit for a /start payload.
	maxPayloadLen = 64
	// MaxCallbackLen is the Telegram limit for callback_data. A nickname label
	// travels inside it, so config validation checks every label against it.
	MaxCallbackLen = 64
)

// DeepLink builds the URL behind the "comment anonymously" button under a post.
func DeepLink(botUsername string, channelMessageID int) string {
	return fmt.Sprintf("https://t.me/%s?start=%s%d",
		strings.TrimPrefix(strings.TrimSpace(botUsername), "@"),
		startPrefix,
		channelMessageID,
	)
}

// ParseStartPayload extracts the channel post id from a /start payload.
func ParseStartPayload(payload string) (int, error) {
	payload = strings.TrimSpace(payload)
	if len(payload) > maxPayloadLen {
		return 0, fmt.Errorf("%w: %d bytes", ErrBadPayload, len(payload))
	}

	raw, ok := strings.CutPrefix(payload, startPrefix)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrBadPayload, payload)
	}

	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: %q", ErrBadPayload, payload)
	}

	return id, nil
}

// NicknameCallback builds the callback data of a nickname button. The label
// itself travels in it rather than a position in the list, so editing the config
// can never make an open keyboard select the wrong mask.
func NicknameCallback(label string) string {
	return nicknamePrefix + label
}

// NicknameCallbackFits reports whether a label survives the callback_data limit.
func NicknameCallbackFits(label string) bool {
	return len(NicknameCallback(label)) <= MaxCallbackLen
}

// ParseNicknameCallback reads a nickname label back from callback data.
func ParseNicknameCallback(data string) (string, error) {
	raw, ok := strings.CutPrefix(data, nicknamePrefix)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: %q", ErrBadPayload, data)
	}

	return raw, nil
}

// FormatBody renders a comment for the discussion group: the mask in bold, then a
// blank line, then the author's words. The result is Telegram HTML, so both parts
// are escaped — a comment containing "<b>" must read as text, not as markup.
func FormatBody(nickname Nickname, text string) string {
	name := "<b>" + html.EscapeString(strings.TrimSpace(nickname.Label)) + "</b>"

	text = strings.TrimSpace(text)
	if text == "" {
		return name
	}

	return name + "\n\n" + html.EscapeString(text)
}

// PostLink builds a link a reader can follow back to the post itself. A public
// channel is addressed by its @username; a private one by the /c/ form, which
// takes the channel id with the -100 supergroup prefix stripped.
func PostLink(post Post, channelID int64) string {
	if name := strings.TrimPrefix(strings.TrimSpace(post.ChannelUsername), "@"); name != "" {
		return fmt.Sprintf("https://t.me/%s/%d", name, post.ChannelMessageID)
	}

	internal := strconv.FormatInt(channelID, 10)
	internal = strings.TrimPrefix(internal, "-100")

	return fmt.Sprintf("https://t.me/c/%s/%d", internal, post.ChannelMessageID)
}
