package comment_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/comment"
)

func TestDeepLink(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://t.me/anon_bot?start=comment_42", comment.DeepLink("anon_bot", 42))
	assert.Equal(t, "https://t.me/anon_bot?start=comment_42", comment.DeepLink(" @anon_bot ", 42))
}

func TestParseStartPayload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
		want    int
		wantErr bool
	}{
		{name: "ok", payload: "comment_42", want: 42},
		{name: "trims spaces", payload: "  comment_7  ", want: 7},
		{name: "round trip", payload: strings.TrimPrefix(comment.DeepLink("b", 123), "https://t.me/b?start="), want: 123},
		{name: "wrong prefix", payload: "suggest_42", wantErr: true},
		{name: "empty", payload: "", wantErr: true},
		{name: "prefix only", payload: "comment_", wantErr: true},
		{name: "not a number", payload: "comment_abc", wantErr: true},
		{name: "zero", payload: "comment_0", wantErr: true},
		{name: "negative", payload: "comment_-1", wantErr: true},
		{name: "too long", payload: "comment_" + strings.Repeat("9", 60), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := comment.ParseStartPayload(tc.payload)
			if tc.wantErr {
				require.ErrorIs(t, err, comment.ErrBadPayload)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCallbackRoundTrip(t *testing.T) {
	t.Parallel()

	// The label travels verbatim, so a config edit cannot shift what a button means.
	for _, label := range []string{"Лис", "Сова", "Ёж", "a b c", "nick:weird"} {
		got, err := comment.ParseNicknameCallback(comment.NicknameCallback(label))
		require.NoError(t, err, label)
		assert.Equal(t, label, got)
	}
}

func TestNicknameCallbackFits(t *testing.T) {
	t.Parallel()

	// "nick:" is 5 bytes of the 64 Telegram allows for callback_data.
	assert.True(t, comment.NicknameCallbackFits("Тушканчик"))
	assert.True(t, comment.NicknameCallbackFits(strings.Repeat("я", 29)+"a"), "59 bytes is the longest that fits")
	assert.False(t, comment.NicknameCallbackFits(strings.Repeat("я", 30)), "60 bytes overflows")
	assert.LessOrEqual(t, len(comment.NicknameCallback(strings.Repeat("я", 29)+"a")), comment.MaxCallbackLen)
}

func TestCallbackParseRejectsForeignData(t *testing.T) {
	t.Parallel()

	// Cancel travels as its own fixed value and must not parse as a mask.
	_, err := comment.ParseNicknameCallback(comment.CancelCallback)
	require.ErrorIs(t, err, comment.ErrBadPayload)

	for _, data := range []string{"", "nick:", "nick:   ", "Лис"} {
		_, err := comment.ParseNicknameCallback(data)
		require.ErrorIs(t, err, comment.ErrBadPayload, data)
	}
}

func TestFormatBody(t *testing.T) {
	t.Parallel()

	nick := comment.Nickname{Label: "Лис"}

	cases := []struct {
		name string
		nick comment.Nickname
		text string
		want string
	}{
		{name: "mask in bold, then a blank line", nick: nick, text: "привет", want: "<b>Лис</b>\n\nпривет"},
		{name: "trims text", nick: nick, text: "  привет  ", want: "<b>Лис</b>\n\nпривет"},
		{name: "media only keeps the mask alone", nick: nick, text: "", want: "<b>Лис</b>"},
		{name: "multiline text survives", nick: nick, text: "one\ntwo", want: "<b>Лис</b>\n\none\ntwo"},
		{
			// Telegram parses the result as HTML, so a comment cannot inject markup.
			name: "markup in the text is escaped",
			nick: nick,
			text: "<b>жирный</b> & <script>",
			want: "<b>Лис</b>\n\n&lt;b&gt;жирный&lt;/b&gt; &amp; &lt;script&gt;",
		},
		{
			name: "markup in the mask is escaped too",
			nick: comment.Nickname{Label: "<i>Лис"},
			text: "привет",
			want: "<b>&lt;i&gt;Лис</b>\n\nпривет",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, comment.FormatBody(tc.nick, tc.text))
		})
	}
}

func TestPostLink(t *testing.T) {
	t.Parallel()

	public := comment.Post{ChannelMessageID: 42, ChannelUsername: "anon_channel"}
	assert.Equal(t, "https://t.me/anon_channel/42", comment.PostLink(public, -1001234567890))

	withAt := comment.Post{ChannelMessageID: 42, ChannelUsername: "@anon_channel"}
	assert.Equal(t, "https://t.me/anon_channel/42", comment.PostLink(withAt, -1001234567890))

	// A private channel has no username, so the link takes the /c/ form with the
	// -100 supergroup prefix stripped off the channel id.
	private := comment.Post{ChannelMessageID: 42}
	assert.Equal(t, "https://t.me/c/1234567890/42", comment.PostLink(private, -1001234567890))
}
