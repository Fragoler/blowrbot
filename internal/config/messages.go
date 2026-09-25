package config

import (
	"fmt"
	"sort"
	"strings"
)

// Messages is every line the bot shows a person. Defaults live in DefaultMessages,
// and go-toml leaves absent keys untouched, so an operator overrides only the
// wording they actually want to change.
type Messages struct {
	ButtonComment  string `toml:"button_comment"`
	ButtonOpenPost string `toml:"button_open_post"`
	ButtonCancel   string `toml:"button_cancel"`
	// ReplyLink is the text of the link under every published comment.
	ReplyLink string `toml:"reply_link"`

	// Invite is the bot's own first comment under every post.
	Invite         string `toml:"invite"`
	Help           string `toml:"help"`
	Prompt         string `toml:"prompt"`
	ReplyPrompt    string `toml:"reply_prompt"`
	ChooseNickname string `toml:"choose_nickname"`
	Published      string `toml:"published"`
	// Achievement is shown when a comment earns one; it takes the title.
	Achievement string `toml:"achievement"`
	Cancelled   string `toml:"cancelled"`

	// Profile is the /profile screen. Counters and lists are filled into it.
	Profile Profile `toml:"profile"`

	Errors Errors `toml:"errors"`
}

// Profile is the wording of the /profile screen. These are plain text: the bot
// adds the bold and italic markup itself and escapes every line, so an operator
// can put emoji here without worrying about breaking Telegram's HTML.
type Profile struct {
	Title            string `toml:"title"`
	Comments         string `toml:"comments"`
	Replies          string `toml:"replies"`
	AchievementsHead string `toml:"achievements_head"`
	NoAchievements   string `toml:"no_achievements"`
	NicknamesHead    string `toml:"nicknames_head"`
}

// Errors is what an author is told when the bot refuses their message. Nothing
// here may leak internal detail: an unmapped failure shows Internal.
type Errors struct {
	Banned         string `toml:"banned"`
	BadPayload     string `toml:"bad_payload"`
	UnknownPost    string `toml:"unknown_post"`
	UnknownComment string `toml:"unknown_comment"`
	NoNicknames    string `toml:"no_nicknames"`
	NoDraft        string `toml:"no_draft"`
	DraftExpired   string `toml:"draft_expired"`
	Nickname       string `toml:"nickname"`
	Empty          string `toml:"empty"`
	TooLong        string `toml:"too_long"`
	NothingStaged  string `toml:"nothing_staged"`
	Unsupported    string `toml:"unsupported"`
	Internal       string `toml:"internal"`
}

// DefaultMessages is the built-in Russian copy. Tests build a config from it, so
// there is exactly one place where the wording lives.
func DefaultMessages() Messages {
	return Messages{
		ButtonComment:  "💬 Комментировать анонимно",
		ButtonOpenPost: "Открыть пост",
		ButtonCancel:   "Отмена",
		ReplyLink:      "ответить",

		Invite: "Комментарии к этому посту анонимные. Нажмите кнопку ниже — и пишите.",
		Help: "Этот бот публикует анонимные комментарии.\n\n" +
			"Нажмите «💬 Комментировать анонимно» под постом в канале — " +
			"и напишите сюда текст или пришлите медиа.",
		Prompt:         "Напишите комментарий к посту — текстом или медиа.",
		ReplyPrompt:    "Напишите ответ — текстом или медиа.",
		ChooseNickname: "Под каким псевдонимом отправить?",
		Published:      "Комментарий отправлен.",
		Achievement:    "🏅 Новое достижение",
		Cancelled:      "Черновик удалён. Напишите новый комментарий.",

		Profile: Profile{
			Title:            "📊 Ваша статистика",
			Comments:         "💬 Комментариев: %d",
			Replies:          "↩️ Ответов: %d",
			AchievementsHead: "🏅 Достижения",
			NoAchievements:   "Пока ни одного — они открывают новые псевдонимы.",
			NicknamesHead:    "🎭 Доступные псевдонимы",
		},

		Errors: Errors{
			Banned:         "Вы не можете оставлять комментарии.",
			BadPayload:     "Ссылка не распознана. Откройте её кнопкой под постом.",
			UnknownPost:    "Пост не найден — возможно, он удалён.",
			UnknownComment: "Комментарий не найден — возможно, он удалён.",
			NoNicknames:    "Список псевдонимов пуст, комментарии временно недоступны.",
			NoDraft:        "Сначала нажмите «💬 Комментировать анонимно» под нужным постом.",
			DraftExpired:   "Время на комментарий истекло. Нажмите кнопку под постом ещё раз.",
			Nickname:       "Этот псевдоним больше недоступен, выберите другой.",
			Empty:          "Пустой комментарий: пришлите текст или медиа.",
			TooLong:        "Комментарий слишком длинный, сократите текст.",
			NothingStaged:  "Нечего отправлять — сначала напишите комментарий.",
			Unsupported:    "Такой тип вложения не поддерживается.",
			Internal:       "Что-то пошло не так, попробуйте ещё раз позже.",
		},
	}
}

// validate rejects a blank line: an empty button or link would render as an
// unusable control rather than fall back to the default.
func (m Messages) validate() []error {
	named := map[string]string{
		"messages.button_comment":            m.ButtonComment,
		"messages.button_open_post":          m.ButtonOpenPost,
		"messages.button_cancel":             m.ButtonCancel,
		"messages.reply_link":                m.ReplyLink,
		"messages.invite":                    m.Invite,
		"messages.help":                      m.Help,
		"messages.prompt":                    m.Prompt,
		"messages.reply_prompt":              m.ReplyPrompt,
		"messages.choose_nickname":           m.ChooseNickname,
		"messages.published":                 m.Published,
		"messages.cancelled":                 m.Cancelled,
		"messages.profile.title":             m.Profile.Title,
		"messages.profile.comments":          m.Profile.Comments,
		"messages.profile.replies":           m.Profile.Replies,
		"messages.profile.achievements_head": m.Profile.AchievementsHead,
		"messages.profile.no_achievements":   m.Profile.NoAchievements,
		"messages.profile.nicknames_head":    m.Profile.NicknamesHead,
		"messages.errors.banned":             m.Errors.Banned,
		"messages.errors.bad_payload":        m.Errors.BadPayload,
		"messages.errors.unknown_post":       m.Errors.UnknownPost,
		"messages.errors.unknown_comment":    m.Errors.UnknownComment,
		"messages.errors.no_nicknames":       m.Errors.NoNicknames,
		"messages.errors.no_draft":           m.Errors.NoDraft,
		"messages.errors.draft_expired":      m.Errors.DraftExpired,
		"messages.errors.nickname":           m.Errors.Nickname,
		"messages.errors.empty":              m.Errors.Empty,
		"messages.errors.too_long":           m.Errors.TooLong,
		"messages.errors.nothing_staged":     m.Errors.NothingStaged,
		"messages.errors.unsupported":        m.Errors.Unsupported,
		"messages.errors.internal":           m.Errors.Internal,
	}

	var errs []error
	for _, key := range sortedKeys(named) {
		if strings.TrimSpace(named[key]) == "" {
			errs = append(errs, fmt.Errorf("%s is empty", key))
		}
	}

	return errs
}

// sortedKeys keeps the reported errors in a stable order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
