package telegram

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/achievement"
	"loudbot/internal/comment"
	"loudbot/internal/config"
	"loudbot/internal/profile"
)

func TestStartPayload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		text   string
		want   string
		wantOk bool
	}{
		{name: "with payload", text: "/start comment_42", want: "comment_42", wantOk: true},
		{name: "bare start", text: "/start", want: "", wantOk: true},
		{name: "trailing spaces", text: "  /start  comment_7 ", want: "comment_7", wantOk: true},
		{name: "plain text", text: "привет", wantOk: false},
		{name: "start inside a word", text: "/startup", wantOk: false},
		{name: "empty", text: "", wantOk: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := startPayload(tc.text)
			assert.Equal(t, tc.wantOk, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNicknameKeyboard(t *testing.T) {
	t.Parallel()

	nicknames := []comment.Nickname{{Label: "Лис"}, {Label: "Сова"}, {Label: "Ёж"}}

	markup := nicknameKeyboard(nicknames, "Отмена")

	require.Len(t, markup.InlineKeyboard, 3, "two per row, then the odd one, then cancel")
	assert.Equal(t, "Лис", markup.InlineKeyboard[0][0].Text)
	assert.Equal(t, "Сова", markup.InlineKeyboard[0][1].Text)
	assert.Equal(t, "Ёж", markup.InlineKeyboard[1][0].Text)

	cancel := markup.InlineKeyboard[2]
	require.Len(t, cancel, 1, "cancel gets a row of its own")
	assert.Equal(t, "Отмена", cancel[0].Text)
	assert.Equal(t, comment.CancelCallback, cancel[0].CallbackData)

	label, err := comment.ParseNicknameCallback(markup.InlineKeyboard[1][0].CallbackData)
	require.NoError(t, err)
	assert.Equal(t, "Ёж", label)

	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			assert.LessOrEqual(t, len(btn.CallbackData), comment.MaxCallbackLen,
				"Telegram drops a button whose callback_data overflows")
		}
	}
}

func TestNicknameKeyboardWithoutMasks(t *testing.T) {
	t.Parallel()

	markup := nicknameKeyboard(nil, "Отмена")
	require.Len(t, markup.InlineKeyboard, 1, "cancel is always offered")
	assert.Equal(t, "Отмена", markup.InlineKeyboard[0][0].Text)
}

func TestPromptText(t *testing.T) {
	t.Parallel()

	msgs := config.DefaultMessages()

	assert.Equal(t, msgs.Prompt, promptText("  ", msgs.Prompt), "a post with no text is not quoted at all")

	quoted := promptText("Текст поста", msgs.Prompt)
	assert.Contains(t, quoted, "<blockquote>Текст поста</blockquote>")
	assert.Contains(t, quoted, msgs.Prompt)

	// The quote is sent as HTML, so a post containing markup must not break it.
	assert.Contains(t, promptText("<b>жирный</b>", msgs.Prompt), "&lt;b&gt;жирный&lt;/b&gt;")

	long := promptText(strings.Repeat("я", quoteLimit+50), msgs.Prompt)
	assert.Contains(t, long, "…", "a long post is cut down to a hint")
	assert.Less(t, utf8.RuneCountInString(long), quoteLimit+len([]rune(msgs.Prompt))+40)
}

func TestStartPrompt(t *testing.T) {
	t.Parallel()

	msgs := config.DefaultMessages()

	onPost := startPrompt(comment.StartResult{Post: comment.Post{Body: "Текст поста"}}, msgs)
	assert.Contains(t, onPost, "<blockquote>Текст поста</blockquote>")
	assert.Contains(t, onPost, msgs.Prompt)

	onComment := startPrompt(comment.StartResult{
		Post:    comment.Post{Body: "Текст поста"},
		ReplyTo: comment.Comment{ID: 7, Nickname: "Сова", Text: "а где продолжение?"},
	}, msgs)
	assert.Contains(t, onComment, "<blockquote>Сова: а где продолжение?</blockquote>",
		"a reply quotes the comment it answers, not the post")
	assert.Contains(t, onComment, msgs.ReplyPrompt)
	assert.NotContains(t, onComment, "Текст поста")
}

func TestMessageBody(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "текст", messageBody(&models.Message{Text: "текст"}))
	assert.Equal(t, "подпись", messageBody(&models.Message{Caption: "подпись"}))
	assert.Empty(t, messageBody(&models.Message{}))
}

func TestExtractMedia(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		msg     *models.Message
		want    []comment.Media
		wantErr bool
	}{
		{
			name: "text only",
			msg:  &models.Message{Text: "привет"},
			want: nil,
		},
		{
			name: "photo takes the largest size",
			msg: &models.Message{Photo: []models.PhotoSize{
				{FileID: "small", FileUniqueID: "us"},
				{FileID: "large", FileUniqueID: "ul"},
			}},
			want: []comment.Media{{Type: comment.MediaPhoto, FileID: "large", FileUniqueID: "ul"}},
		},
		{
			name: "video",
			msg:  &models.Message{Video: &models.Video{FileID: "v", FileUniqueID: "uv"}},
			want: []comment.Media{{Type: comment.MediaVideo, FileID: "v", FileUniqueID: "uv"}},
		},
		{
			name: "document",
			msg:  &models.Message{Document: &models.Document{FileID: "d", FileUniqueID: "ud"}},
			want: []comment.Media{{Type: comment.MediaDocument, FileID: "d", FileUniqueID: "ud"}},
		},
		{
			name:    "sticker is refused",
			msg:     &models.Message{Sticker: &models.Sticker{FileID: "s"}},
			wantErr: true,
		},
		{
			name:    "voice is refused",
			msg:     &models.Message{Voice: &models.Voice{FileID: "vo"}},
			wantErr: true,
		},
		{
			name: "empty message carries nothing",
			msg:  &models.Message{},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractMedia(tc.msg)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestUserMessage(t *testing.T) {
	t.Parallel()

	msgs := config.DefaultMessages()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "guard reason reaches the author", err: &comment.RejectedError{Reason: "слишком часто"}, want: "слишком часто"},
		{name: "nothing staged", err: comment.ErrNothingStaged, want: msgs.Errors.NothingStaged},
		{name: "deleted parent comment", err: comment.ErrUnknownComment, want: msgs.Errors.UnknownComment},
		{name: "banned", err: comment.ErrBanned, want: msgs.Errors.Banned},
		{name: "wrapped sentinel", err: errors.Join(errors.New("ctx"), comment.ErrDraftExpired), want: msgs.Errors.DraftExpired},
		{name: "no draft", err: comment.ErrNoDraft, want: msgs.Errors.NoDraft},
		{name: "internal errors stay opaque", err: errors.New("pq: connection refused"), want: msgs.Errors.Internal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, userMessage(msgs, tc.err))
		})
	}
}

func TestExpected(t *testing.T) {
	t.Parallel()

	assert.True(t, expected(comment.ErrEmptyComment))
	assert.True(t, expected(comment.ErrNothingStaged))
	assert.True(t, expected(comment.ErrUnknownComment))
	assert.True(t, expected(&comment.RejectedError{Reason: "стоп-слово"}))
	assert.False(t, expected(errors.New("boom")), "a real failure must still be logged as an error")
}

func TestProfileText(t *testing.T) {
	t.Parallel()

	m := config.DefaultMessages().Profile

	got := profileText(profile.Profile{
		Activity: profile.Activity{Comments: 12, Replies: 4},
		Achievements: []profile.Achievement{
			{Title: "Полуночник", Description: "Длинный комментарий ночью"},
			{Title: "Крикун"},
		},
		Nicknames: []comment.Nickname{{Label: "Лис"}, {Label: "Сова"}},
	}, m)

	assert.Contains(t, got, "<b>📊 Ваша статистика</b>")
	assert.Contains(t, got, "💬 Комментариев: 12")
	assert.Contains(t, got, "↩️ Ответов: 4")
	assert.Contains(t, got, "<b>🏅 Достижения (2)</b>", "a heading says how long its list is")
	assert.Contains(t, got, "• <b>Полуночник</b>")
	assert.Contains(t, got, "<i>Длинный комментарий ночью</i>")
	assert.Contains(t, got, "• <b>Крикун</b>")
	assert.NotContains(t, got, "Крикун</b>\n   <i>", "an achievement with no description gets no empty line")
	assert.Contains(t, got, "<b>🎭 Доступные псевдонимы (2)</b>")
	assert.Contains(t, got, "• Лис")
	assert.False(t, strings.HasSuffix(got, "\n"), "no trailing blank line")
}

func TestProfileTextWithNothingEarned(t *testing.T) {
	t.Parallel()

	m := config.DefaultMessages().Profile

	got := profileText(profile.Profile{Nicknames: []comment.Nickname{{Label: "Лис"}}}, m)

	assert.Contains(t, got, "<b>🏅 Достижения</b>", "an empty list drops the count")
	assert.NotContains(t, got, "Достижения (0)")
	assert.Contains(t, got, "<i>"+m.NoAchievements+"</i>")
}

func TestProfileTextEscapesWhatItDidNotWrite(t *testing.T) {
	t.Parallel()

	// Titles and masks come from the database; raw markup there must not reach
	// Telegram as markup, or the whole message fails to parse.
	got := profileText(profile.Profile{
		Achievements: []profile.Achievement{{Title: "<b>взлом", Description: "a & b"}},
		Nicknames:    []comment.Nickname{{Label: "<i>Лис"}},
	}, config.DefaultMessages().Profile)

	assert.Contains(t, got, "&lt;b&gt;взлом")
	assert.Contains(t, got, "a &amp; b")
	assert.Contains(t, got, "&lt;i&gt;Лис")
}

func TestAchievementText(t *testing.T) {
	t.Parallel()

	head := config.DefaultMessages().Achievement

	withDescription := achievementText(head, achievement.Granted{
		Title:       "Полуночник",
		Description: "Длинный комментарий ночью",
	})
	assert.Equal(t,
		"<b>🏅 Новое достижение</b>\n\n<b>Полуночник</b>\n<i>Длинный комментарий ночью</i>",
		withDescription)

	bare := achievementText(head, achievement.Granted{Title: "Крикун"})
	assert.Equal(t, "<b>🏅 Новое достижение</b>\n\n<b>Крикун</b>", bare)

	escaped := achievementText(head, achievement.Granted{Title: "<b>взлом"})
	assert.Contains(t, escaped, "&lt;b&gt;взлом")
}
