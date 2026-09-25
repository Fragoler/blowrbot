package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"unicode/utf8"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"loudbot/internal/comment"
)

const (
	startCommand = "/start"
	// quoteLimit keeps the quoted post short enough to stay a hint rather than a
	// wall of text above the author's own message.
	quoteLimit = 280
)

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
		Body:                messageBody(msg),
		ChannelUsername:     channel.Chat.Username,
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

// onStart greets an author arriving from a post: it quotes the post, links back
// to it, and asks for the comment. The mask is chosen later.
func (b *Bot) onStart(ctx context.Context, msg *models.Message, payload string) {
	userID := msg.From.ID

	if payload == "" {
		b.reply(ctx, msg.Chat.ID, msgHelp)

		return
	}

	started, err := b.comments.Start(ctx, userID, payload)
	if err != nil {
		b.replyError(ctx, msg.Chat.ID, "start comment draft", userID, err)

		return
	}

	params := &tgbot.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      startPrompt(started),
		ParseMode: models.ParseModeHTML,
	}

	if started.Link != "" {
		params.ReplyMarkup = models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{{
				{Text: btnOpen, URL: started.Link},
			}},
		}
	}

	if _, err := b.api.SendMessage(ctx, params); err != nil {
		b.log.Error("send comment prompt", slog.Int64("user_id", userID), slog.Any("error", err))
	}
}

// onComment stages what the author wrote and asks which mask to sign it with.
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

	staged, err := b.comments.Stage(ctx, comment.StageRequest{
		UserID:    userID,
		MessageID: msg.ID,
		Text:      text,
		Media:     media,
	})
	if err != nil {
		b.replyError(ctx, msg.Chat.ID, "stage comment", userID, err)

		return
	}

	// Writing again instead of picking a mask replaces the previous draft, so the
	// keyboard that belonged to it goes away with it.
	b.deleteMessage(ctx, msg.Chat.ID, staged.StalePromptID)

	prompt, err := b.api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        msgChooseNickname,
		ReplyMarkup: nicknameKeyboard(staged.Nicknames),
	})
	if err != nil {
		b.log.Error("send nickname keyboard", slog.Int64("user_id", userID), slog.Any("error", err))

		return
	}

	if err := b.comments.AttachPrompt(ctx, userID, prompt.ID); err != nil {
		b.log.Error("attach nickname keyboard", slog.Int64("user_id", userID), slog.Any("error", err))
	}
}

func (b *Bot) onCallback(ctx context.Context, query *models.CallbackQuery) {
	switch {
	case query.Data == comment.CancelCallback:
		b.onCancel(ctx, query)
	case strings.HasPrefix(query.Data, "nick:"):
		b.onNicknameChosen(ctx, query)
	default:
		b.answer(ctx, query.ID, "")
	}
}

// onNicknameChosen signs the staged message and sends it into the thread.
func (b *Bot) onNicknameChosen(ctx context.Context, query *models.CallbackQuery) {
	label, err := comment.ParseNicknameCallback(query.Data)
	if err != nil {
		b.answer(ctx, query.ID, msgErrBadPayload)

		return
	}

	published, err := b.comments.Publish(ctx, query.From.ID, label)
	if err != nil {
		b.logCoreError("publish comment", query.From.ID, err)
		b.answer(ctx, query.ID, userMessage(err))

		return
	}

	b.log.Info("comment published",
		slog.Int64("comment_id", published.ID),
		slog.Int("post_id", published.PostID),
		slog.Int("message_id", published.MessageID),
	)

	b.answer(ctx, query.ID, msgPublished)

	// The keyboard has done its job; turning it into the confirmation keeps the
	// private chat from filling up with dead buttons.
	b.replacePrompt(ctx, query, msgPublished)
}

// onCancel drops the staged message, taking both it and the keyboard off screen.
func (b *Bot) onCancel(ctx context.Context, query *models.CallbackQuery) {
	cancelled, err := b.comments.Cancel(ctx, query.From.ID)
	if err != nil {
		b.logCoreError("cancel comment", query.From.ID, err)
		b.answer(ctx, query.ID, userMessage(err))

		return
	}

	b.answer(ctx, query.ID, msgCancelled)

	b.deleteMessage(ctx, cancelled.ChatID, cancelled.UserMessageID)
	b.deleteMessage(ctx, cancelled.ChatID, cancelled.PromptMessageID)
}

// replacePrompt rewrites the keyboard message in place, dropping its buttons.
func (b *Bot) replacePrompt(ctx context.Context, query *models.CallbackQuery, text string) {
	if query.Message.Message == nil {
		return
	}

	_, err := b.api.EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:    query.Message.Message.Chat.ID,
		MessageID: query.Message.Message.ID,
		Text:      text,
	})
	if err != nil {
		b.log.Debug("replace nickname keyboard", slog.Any("error", err))
	}
}

func (b *Bot) deleteMessage(ctx context.Context, chatID int64, messageID int) {
	if messageID == 0 {
		return
	}

	_, err := b.api.DeleteMessage(ctx, &tgbot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	if err != nil {
		b.log.Debug("delete message",
			slog.Int64("chat_id", chatID),
			slog.Int("message_id", messageID),
			slog.Any("error", err),
		)
	}
}

// startPayload splits "/start <payload>"; ok is false for any other text.
func startPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text != startCommand && !strings.HasPrefix(text, startCommand+" ") {
		return "", false
	}

	return strings.TrimSpace(strings.TrimPrefix(text, startCommand)), true
}

// startPrompt asks for the text, quoting whatever the author is answering — the
// post, or the comment they opened through its "ответить" link.
func startPrompt(started comment.StartResult) string {
	if started.IsReply() {
		return promptText(started.ReplyTo.Nickname+": "+started.ReplyTo.Text, msgReplyPrompt)
	}

	return promptText(started.Post.Body, msgPrompt)
}

// promptText puts a short quote of the source above the instruction.
func promptText(source, ask string) string {
	quote := truncate(strings.TrimSpace(source), quoteLimit)
	if quote == "" {
		return ask
	}

	return "<blockquote>" + html.EscapeString(quote) + "</blockquote>\n" + ask
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}

	return string([]rune(s)[:limit]) + "…"
}

// nicknameKeyboard lays the masks out two per row, with cancel on its own row.
func nicknameKeyboard(nicknames []comment.Nickname) models.InlineKeyboardMarkup {
	const perRow = 2

	rows := make([][]models.InlineKeyboardButton, 0, len(nicknames)/perRow+2)
	row := make([]models.InlineKeyboardButton, 0, perRow)

	for _, n := range nicknames {
		row = append(row, models.InlineKeyboardButton{
			Text:         n.Label,
			CallbackData: comment.NicknameCallback(n.Label),
		})

		if len(row) == perRow {
			rows = append(rows, row)
			row = make([]models.InlineKeyboardButton, 0, perRow)
		}
	}

	if len(row) > 0 {
		rows = append(rows, row)
	}

	rows = append(rows, []models.InlineKeyboardButton{
		{Text: btnCancel, CallbackData: comment.CancelCallback},
	})

	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// messageBody is the text of a message, or the caption when it carries media.
func messageBody(msg *models.Message) string {
	if msg.Text != "" {
		return msg.Text
	}

	return msg.Caption
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
