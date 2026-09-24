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

	nick, err := comment.ParseNicknameCallback(comment.NicknameCallback(17))
	require.NoError(t, err)
	assert.Equal(t, int64(17), nick)

	report, err := comment.ParseReportCallback(comment.ReportCallback(99))
	require.NoError(t, err)
	assert.Equal(t, int64(99), report)
}

func TestCallbackParseRejectsForeignData(t *testing.T) {
	t.Parallel()

	// A nickname parser must not accept a report payload and vice versa.
	_, err := comment.ParseNicknameCallback(comment.ReportCallback(5))
	require.ErrorIs(t, err, comment.ErrBadPayload)

	_, err = comment.ParseReportCallback(comment.NicknameCallback(5))
	require.ErrorIs(t, err, comment.ErrBadPayload)

	for _, data := range []string{"", "nick:", "nick:abc", "nick:0", "nick:-3", "report:x"} {
		_, err := comment.ParseNicknameCallback(data)
		if strings.HasPrefix(data, "nick:") {
			require.ErrorIs(t, err, comment.ErrBadPayload, data)
		}

		_, err = comment.ParseReportCallback(data)
		require.Error(t, err, data)
	}
}

func TestNicknameDisplay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		nick comment.Nickname
		want string
	}{
		{name: "emoji and label", nick: comment.Nickname{Emoji: "🦊", Label: "Лис"}, want: "🦊 Лис"},
		{name: "label only", nick: comment.Nickname{Label: "Лис"}, want: "Лис"},
		{name: "emoji only", nick: comment.Nickname{Emoji: "🦊"}, want: "🦊"},
		{name: "trims", nick: comment.Nickname{Emoji: " 🦊 ", Label: " Лис "}, want: "🦊 Лис"},
		{name: "empty", nick: comment.Nickname{}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.nick.Display())
		})
	}
}

func TestFormatBody(t *testing.T) {
	t.Parallel()

	nick := comment.Nickname{Emoji: "🦊", Label: "Лис"}

	cases := []struct {
		name string
		nick comment.Nickname
		text string
		want string
	}{
		{name: "text", nick: nick, text: "привет", want: "🦊 Лис: привет"},
		{name: "trims text", nick: nick, text: "  привет  ", want: "🦊 Лис: привет"},
		{name: "media only keeps the mask alone", nick: nick, text: "", want: "🦊 Лис"},
		{name: "blank mask falls back to text", nick: comment.Nickname{}, text: "привет", want: "привет"},
		{name: "multiline text", nick: nick, text: "one\ntwo", want: "🦊 Лис: one\ntwo"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, comment.FormatBody(tc.nick, tc.text, ": "))
		})
	}
}
