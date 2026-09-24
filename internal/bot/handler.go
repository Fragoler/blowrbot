package bot

import (
	"context"
	"log/slog"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// handleUpdate — единственный обработчик апдейтов. Пока умеет только
// оставлять первый комментарий под новым постом канала.
func (b *Bot) handleUpdate(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
	post := channelPostInDiscussion(update)
	if post == nil {
		return
	}

	if b.cfg.ChannelID != 0 && post.SenderChat.ID != b.cfg.ChannelID {
		b.log.Debug("пост из чужого канала, пропускаем",
			slog.Int64("sender_chat_id", post.SenderChat.ID),
		)

		return
	}

	b.commentFirst(ctx, post)
}

// commentFirst отвечает на пересланный в чат обсуждений пост, тем самым
// создавая первый комментарий к нему.
func (b *Bot) commentFirst(ctx context.Context, post *models.Message) {
	log := b.log.With(
		slog.Int64("chat_id", post.Chat.ID),
		slog.Int("message_id", post.ID),
	)

	sent, err := b.api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: post.Chat.ID,
		Text:   b.cfg.FirstComment,
		ReplyParameters: &models.ReplyParameters{
			ChatID:    post.Chat.ID,
			MessageID: post.ID,
		},
	})
	if err != nil {
		log.Error("не удалось отправить первый комментарий", slog.Any("error", err))

		return
	}

	log.Info("первый комментарий отправлен", slog.Int("comment_id", sent.ID))
}

// channelPostInDiscussion возвращает сообщение, если апдейт — это пост канала,
// автоматически пересланный в связанный чат обсуждений. Именно на такое
// сообщение нужно отвечать, чтобы ответ попал в комментарии к посту.
func channelPostInDiscussion(update *models.Update) *models.Message {
	msg := update.Message
	if msg == nil || !msg.IsAutomaticForward || msg.SenderChat == nil {
		return nil
	}

	return msg
}
