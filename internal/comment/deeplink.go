package comment

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// startPrefix marks a /start payload that opens a comment draft.
	startPrefix = "comment_"
	// nicknamePrefix and reportPrefix namespace the inline callback data.
	nicknamePrefix = "nick:"
	reportPrefix   = "report:"

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

// ReportCallback builds the callback data of the "report" button under a comment.
func ReportCallback(commentID int64) string {
	return reportPrefix + strconv.FormatInt(commentID, 10)
}

// ParseReportCallback reads a comment id back from callback data.
func ParseReportCallback(data string) (int64, error) {
	return parseIDCallback(data, reportPrefix)
}

func parseIDCallback(data, prefix string) (int64, error) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(data), prefix)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrBadPayload, data)
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: %q", ErrBadPayload, data)
	}

	return id, nil
}

// FormatBody prefixes the comment text with the chosen mask. A media-only comment
// carries just the mask as its caption.
func FormatBody(nickname Nickname, text, separator string) string {
	name := nickname.Display()

	text = strings.TrimSpace(text)
	if text == "" {
		return name
	}

	if name == "" {
		return text
	}

	return name + separator + text
}
