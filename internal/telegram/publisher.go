package telegram

import (
	"context"
	"fmt"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"loudbot/internal/comment"
	"loudbot/internal/config"
)

// Publisher sends approved comments into the discussion group. It implements
// comment.Publisher and is the only place that knows how a comment looks in chat.
type Publisher struct {
	api      *tgbot.Bot
	messages config.Messages
}

func NewPublisher(api *tgbot.Bot, messages config.Messages) *Publisher {
	return &Publisher{api: api, messages: messages}
}

// Publisher returns the bot's own publisher, so that main can wire the core
// service after the client exists.
func (b *Bot) Publisher() *Publisher {
	return NewPublisher(b.api, b.cfg.Messages)
}

// PublishInvite leaves the bot's first comment under a fresh post: the thread
// anchor readers tap to write anonymously.
func (p *Publisher) PublishInvite(ctx context.Context, req comment.InviteRequest) (comment.PublishResult, error) {
	markup := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: p.messages.ButtonComment, URL: req.DeepLink},
		}},
	}

	msg, err := p.api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: req.ChatID,
		Text:   p.messages.Invite,
		ReplyParameters: &models.ReplyParameters{
			ChatID:    req.ChatID,
			MessageID: req.ReplyToMessageID,
		},
		ReplyMarkup: markup,
	})
	if err != nil {
		return comment.PublishResult{}, err
	}

	return comment.PublishResult{MessageID: msg.ID}, nil
}

func (p *Publisher) PublishComment(ctx context.Context, req comment.PublishRequest) (comment.PublishResult, error) {
	reply := &models.ReplyParameters{
		ChatID:    req.ChatID,
		MessageID: req.ReplyToMessageID,
	}

	msg, err := p.send(ctx, req, reply)
	if err != nil {
		return comment.PublishResult{}, err
	}

	return comment.PublishResult{MessageID: msg.ID}, nil
}

// send renders the comment. Text is already HTML — the mask in bold, a blank
// line, then the author's escaped words — so every branch sets the parse mode.
func (p *Publisher) send(
	ctx context.Context,
	req comment.PublishRequest,
	reply *models.ReplyParameters,
) (*models.Message, error) {
	if len(req.Media) == 0 {
		return p.api.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:          req.ChatID,
			Text:            req.Text,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: reply,
		})
	}

	media := req.Media[0]
	file := &models.InputFileString{Data: media.FileID}

	switch media.Type {
	case comment.MediaPhoto:
		return p.api.SendPhoto(ctx, &tgbot.SendPhotoParams{
			ChatID:          req.ChatID,
			Photo:           file,
			Caption:         req.Text,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: reply,
		})

	case comment.MediaVideo:
		return p.api.SendVideo(ctx, &tgbot.SendVideoParams{
			ChatID:          req.ChatID,
			Video:           file,
			Caption:         req.Text,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: reply,
		})

	case comment.MediaDocument:
		return p.api.SendDocument(ctx, &tgbot.SendDocumentParams{
			ChatID:          req.ChatID,
			Document:        file,
			Caption:         req.Text,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: reply,
		})

	default:
		return nil, fmt.Errorf("unsupported media type %q", media.Type)
	}
}
