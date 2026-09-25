package telegram

import (
	"errors"

	"loudbot/internal/comment"
)

// User-facing strings. Comments and logs stay in English; these are product copy
// for a Russian-speaking channel.
const (
	btnComment = "💬 Комментировать анонимно"
	btnOpen    = "Открыть пост"
	btnCancel  = "Отмена"

	// msgInvite is the bot's first comment under every post; config.Comments.InviteText overrides it.
	msgInvite = "Комментарии к этому посту анонимные. Нажмите кнопку ниже — и пишите."

	msgHelp = "Этот бот публикует анонимные комментарии.\n\n" +
		"Нажмите «" + btnComment + "» под постом в канале — и напишите сюда текст или пришлите медиа."

	// msgPrompt and msgReplyPrompt greet an author who just arrived; the quote of
	// what they are answering is prepended to them.
	msgPrompt      = "Напишите комментарий к посту — текстом или медиа."
	msgReplyPrompt = "Напишите ответ — текстом или медиа."

	msgChooseNickname = "Под каким псевдонимом отправить?"
	msgPublished      = "Комментарий отправлен."
	msgCancelled      = "Черновик удалён. Напишите новый комментарий."

	msgErrBanned       = "Вы не можете оставлять комментарии."
	msgErrBadPayload   = "Ссылка не распознана. Откройте её кнопкой под постом."
	msgErrUnknownPost  = "Пост не найден — возможно, он удалён."
	msgErrUnknownComm  = "Комментарий не найден — возможно, он удалён."
	msgErrNoNicknames  = "Список псевдонимов пуст, комментарии временно недоступны."
	msgErrNoDraft      = "Сначала нажмите «" + btnComment + "» под нужным постом."
	msgErrDraftExpired = "Время на комментарий истекло. Нажмите кнопку под постом ещё раз."
	msgErrNickname     = "Этот псевдоним больше недоступен, выберите другой."
	msgErrEmpty        = "Пустой комментарий: пришлите текст или медиа."
	msgErrTooLong      = "Комментарий слишком длинный, сократите текст."
	msgErrNothing      = "Нечего отправлять — сначала напишите комментарий."
	msgErrUnsupported  = "Такой тип вложения не поддерживается."
	msgErrInternal     = "Что-то пошло не так, попробуйте ещё раз позже."
)

// userMessage maps a core error onto the text the author sees. An unmapped error
// is internal and must not leak its details.
func userMessage(err error) string {
	if rejected, ok := comment.Rejected(err); ok {
		return rejected.Reason
	}

	switch {
	case errors.Is(err, comment.ErrBanned):
		return msgErrBanned
	case errors.Is(err, comment.ErrBadPayload):
		return msgErrBadPayload
	case errors.Is(err, comment.ErrUnknownPost):
		return msgErrUnknownPost
	case errors.Is(err, comment.ErrUnknownComment):
		return msgErrUnknownComm
	case errors.Is(err, comment.ErrNoNicknames):
		return msgErrNoNicknames
	case errors.Is(err, comment.ErrNoDraft):
		return msgErrNoDraft
	case errors.Is(err, comment.ErrDraftExpired):
		return msgErrDraftExpired
	case errors.Is(err, comment.ErrNicknameUnavailable):
		return msgErrNickname
	case errors.Is(err, comment.ErrEmptyComment):
		return msgErrEmpty
	case errors.Is(err, comment.ErrTextTooLong):
		return msgErrTooLong
	case errors.Is(err, comment.ErrNothingStaged):
		return msgErrNothing
	default:
		return msgErrInternal
	}
}

// expected reports whether the error is a normal outcome of user input rather
// than a failure worth an error-level log line.
func expected(err error) bool {
	if _, ok := comment.Rejected(err); ok {
		return true
	}

	for _, sentinel := range []error{
		comment.ErrBanned,
		comment.ErrBadPayload,
		comment.ErrUnknownPost,
		comment.ErrUnknownComment,
		comment.ErrNoNicknames,
		comment.ErrNoDraft,
		comment.ErrDraftExpired,
		comment.ErrNicknameUnavailable,
		comment.ErrEmptyComment,
		comment.ErrTextTooLong,
		comment.ErrNothingStaged,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}

	return false
}
