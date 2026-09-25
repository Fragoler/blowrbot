package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"loudbot/internal/comment"
)

const startCommand = "/start"

func (b *Bot) handleUpdate(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
	switch {
	case update.ChannelPost != nil:
		b.onChannelPost(ctx, update.ChannelPost)
	case update.CallbackQuery != nil:
		b.onCallback(ctx, update.CallbackQuery)
	case update.Message != nil && update.Message.IsAutomaticForward:
		b.onDiscussionForward(ctx, update.Message)
	case update.Message != nil && update.Message.Chat.Type == models.ChatTypePrivate:
		b.onPrivateMessage(ctx, update.Message)
	}
}

// onChannelPost attaches the "comment anonymously" deep link to a fresh post.
func (b *Bot) onChannelPost(ctx context.Context, post *models.Message) {
	if post.Chat.ID != b.cfg.Telegram.ChannelID {
		return
	}

	markup := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: btnComment, URL: b.comments.DeepLink(post.ID)},
		}},
	}

	_, err := b.api.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
		ChatID:      post.Chat.ID,
		MessageID:   post.ID,
		ReplyMarkup: markup,
	})
	if err != nil {
		b.log.Error("attach comment button", slog.Int("post_id", post.ID), slog.Any("error", err))
	}
}

// onDiscussionForward is where a new post really becomes commentable: the copy
// Telegram auto-forwards here is the anchor every comment replies to, and the
// bot's own first comment under it carries the button readers tap.
func (b *Bot) onDiscussionForward(ctx context.Context, msg *models.Message) {
	if msg.Chat.ID != b.cfg.Telegram.DiscussionChatID {
		return
	}

	origin := msg.ForwardOrigin
	if origin == nil || origin.MessageOriginChannel == nil {
		return
	}

	channel := origin.MessageOriginChannel
	if channel.Chat.ID != b.cfg.Telegram.ChannelID {
		return
	}

	post := comment.Post{
		ChannelMessageID:    channel.MessageID,
		DiscussionChatID:    msg.Chat.ID,
		DiscussionMessageID: msg.ID,
	}

	if err := b.comments.OnPostPublished(ctx, post); err != nil {
		b.log.Error("announce post in discussion thread",
			slog.Int("post_id", post.ChannelMessageID),
			slog.Any("error", err),
		)

		return
	}

	b.log.Info("post announced in discussion thread",
		slog.Int("post_id", post.ChannelMessageID),
		slog.Int("thread_message_id", post.DiscussionMessageID),
	)
}

func (b *Bot) onPrivateMessage(ctx context.Context, msg *models.Message) {
	if msg.From == nil {
		return
	}

	if payload, ok := startPayload(msg.Text); ok {
		b.onStart(ctx, msg, payload)

		return
	}

	b.onComment(ctx, msg)
}

func (b *Bot) onStart(ctx context.Context, msg *models.Message, payload string) {
	userID := msg.From.ID

	if payload == "" {
		b.reply(ctx, msg.Chat.ID, msgHelp)

		return
	}

	result, err := b.comments.Start(ctx, userID, payload)
	if err != nil {
		b.replyError(ctx, msg.Chat.ID, "start comment draft", userID, err)

		return
	}

	_, err = b.api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        msgChooseNickname,
		ReplyMarkup: nicknameKeyboard(result.Nicknames, result.Selected.Label),
	})
	if err != nil {
		b.log.Error("send nickname keyboard", slog.Int64("user_id", userID), slog.Any("error", err))
	}
}

func (b *Bot) onComment(ctx context.Context, msg *models.Message) {
	userID := msg.From.ID

	media, err := extractMedia(msg)
	if err != nil {
		b.reply(ctx, msg.Chat.ID, msgErrUnsupported)

		return
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	published, err := b.comments.Submit(ctx, comment.SubmitRequest{
		UserID: userID,
		Text:   text,
		Media:  media,
	})
	if err != nil {
		b.replyError(ctx, msg.Chat.ID, "submit comment", userID, err)

		return
	}

	b.log.Info("comment published",
		slog.Int64("comment_id", published.ID),
		slog.Int("post_id", published.PostID),
		slog.Int("message_id", published.MessageID),
	)

	mask := published.Nickname
	if n, err := b.comments.Nickname(published.Nickname); err == nil {
		mask = n.Display()
	}

	b.reply(ctx, msg.Chat.ID, fmt.Sprintf(msgPublished, mask))
}

func (b *Bot) onCallback(ctx context.Context, query *models.CallbackQuery) {
	switch {
	case strings.HasPrefix(query.Data, "nick:"):
		b.onNicknameChosen(ctx, query)
	case strings.HasPrefix(query.Data, "report:"):
		b.onReport(ctx, query)
	default:
		b.answer(ctx, query.ID, "")
	}
}

func (b *Bot) onNicknameChosen(ctx context.Context, query *models.CallbackQuery) {
	label, err := comment.ParseNicknameCallback(query.Data)
	if err != nil {
		b.answer(ctx, query.ID, msgErrBadPayload)

		return
	}

	nickname, err := b.comments.ChooseNickname(ctx, query.From.ID, label)
	if err != nil {
		b.logCoreError("choose nickname", query.From.ID, err)
		b.answer(ctx, query.ID, userMessage(err))

		return
	}

	b.answer(ctx, query.ID, fmt.Sprintf(msgNicknameSet, nickname.Display()))
	b.refreshNicknameKeyboard(ctx, query, nickname.Label)
}

// refreshNicknameKeyboard re-renders the keyboard so the chosen mask is ticked.
func (b *Bot) refreshNicknameKeyboard(ctx context.Context, query *models.CallbackQuery, selected string) {
	if query.Message.Message == nil {
		return
	}

	nicknames, err := b.comments.Nicknames()
	if err != nil {
		b.log.Error("reload nicknames", slog.Any("error", err))

		return
	}

	_, err = b.api.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
		ChatID:      query.Message.Message.Chat.ID,
		MessageID:   query.Message.Message.ID,
		ReplyMarkup: nicknameKeyboard(nicknames, selected),
	})
	if err != nil {
		b.log.Debug("refresh nickname keyboard", slog.Any("error", err))
	}
}

func (b *Bot) onReport(ctx context.Context, query *models.CallbackQuery) {
	commentID, err := comment.ParseReportCallback(query.Data)
	if err != nil {
		b.answer(ctx, query.ID, msgErrBadPayload)

		return
	}

	if err := b.store.SaveReport(ctx, "comment", commentID, query.From.ID, ""); err != nil {
		b.log.Error("save report", slog.Int64("comment_id", commentID), slog.Any("error", err))
		b.answer(ctx, query.ID, msgReportFailed)

		return
	}

	b.log.Info("comment reported",
		slog.Int64("comment_id", commentID),
		slog.Int64("reporter_id", query.From.ID),
	)

	b.answer(ctx, query.ID, msgReportAccepted)
}

// startPayload splits "/start <payload>"; ok is false for any other text.
func startPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text != startCommand && !strings.HasPrefix(text, startCommand+" ") {
		return "", false
	}

	return strings.TrimSpace(strings.TrimPrefix(text, startCommand)), true
}

func nicknameKeyboard(nicknames []comment.Nickname, selected string) models.InlineKeyboardMarkup {
	const perRow = 2

	rows := make([][]models.InlineKeyboardButton, 0, len(nicknames)/perRow+1)
	row := make([]models.InlineKeyboardButton, 0, perRow)

	for _, n := range nicknames {
		text := n.Display()
		if n.Label == selected {
			text = "✅ " + text
		}

		row = append(row, models.InlineKeyboardButton{Text: text, CallbackData: comment.NicknameCallback(n.Label)})
		if len(row) == perRow {
			rows = append(rows, row)
			row = make([]models.InlineKeyboardButton, 0, perRow)
		}
	}

	if len(row) > 0 {
		rows = append(rows, row)
	}

	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// extractMedia pulls the reusable file ids out of a message. Telegram keeps the
// files, so the bot only ever stores and re-sends their ids.
func extractMedia(msg *models.Message) ([]comment.Media, error) {
	switch {
	case len(msg.Photo) > 0:
		// The last size is the largest one Telegram offers.
		largest := msg.Photo[len(msg.Photo)-1]

		return []comment.Media{{
			Type:         comment.MediaPhoto,
			FileID:       largest.FileID,
			FileUniqueID: largest.FileUniqueID,
		}}, nil

	case msg.Video != nil:
		return []comment.Media{{
			Type:         comment.MediaVideo,
			FileID:       msg.Video.FileID,
			FileUniqueID: msg.Video.FileUniqueID,
		}}, nil

	case msg.Document != nil:
		return []comment.Media{{
			Type:         comment.MediaDocument,
			FileID:       msg.Document.FileID,
			FileUniqueID: msg.Document.FileUniqueID,
		}}, nil

	case msg.Text != "":
		return nil, nil

	case msg.Sticker != nil, msg.Voice != nil, msg.Audio != nil, msg.VideoNote != nil, msg.Animation != nil:
		return nil, fmt.Errorf("unsupported attachment")

	default:
		return nil, nil
	}
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if _, err := b.api.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		b.log.Error("send reply", slog.Int64("chat_id", chatID), slog.Any("error", err))
	}
}

func (b *Bot) replyError(ctx context.Context, chatID int64, op string, userID int64, err error) {
	b.logCoreError(op, userID, err)
	b.reply(ctx, chatID, userMessage(err))
}

func (b *Bot) logCoreError(op string, userID int64, err error) {
	if expected(err) {
		b.log.Debug(op+" refused", slog.Int64("user_id", userID), slog.Any("reason", err))

		return
	}

	b.log.Error(op+" failed", slog.Int64("user_id", userID), slog.Any("error", err))
}

func (b *Bot) answer(ctx context.Context, queryID, text string) {
	_, err := b.api.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
		CallbackQueryID: queryID,
		Text:            text,
	})
	if err != nil {
		b.log.Debug("answer callback", slog.Any("error", err))
	}
}
