package telegram

import (
	"context"
	"fmt"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"loudbot/internal/comment"
)

// Publisher sends approved comments into the discussion group. It implements
// comment.Publisher and is the only place that knows how a comment looks in chat.
type Publisher struct {
	api        *tgbot.Bot
	inviteText string
}

func NewPublisher(api *tgbot.Bot, inviteText string) *Publisher {
	if strings.TrimSpace(inviteText) == "" {
		inviteText = msgInvite
	}

	return &Publisher{api: api, inviteText: inviteText}
}

// Publisher returns the bot's own publisher, so that main can wire the core
// service after the client exists.
func (b *Bot) Publisher() *Publisher {
	return NewPublisher(b.api, b.cfg.Comments.InviteText)
}

// PublishInvite leaves the bot's first comment under a fresh post: the thread
// anchor readers tap to write anonymously.
func (p *Publisher) PublishInvite(ctx context.Context, req comment.InviteRequest) (comment.PublishResult, error) {
	markup := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: btnComment, URL: req.DeepLink},
		}},
	}

	msg, err := p.api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: req.ChatID,
		Text:   p.inviteText,
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

	markup := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: btnReport, CallbackData: comment.ReportCallback(req.CommentID)},
		}},
	}

	msg, err := p.send(ctx, req, reply, markup)
	if err != nil {
		return comment.PublishResult{}, err
	}

	return comment.PublishResult{MessageID: msg.ID}, nil
}

func (p *Publisher) send(
	ctx context.Context,
	req comment.PublishRequest,
	reply *models.ReplyParameters,
	markup models.InlineKeyboardMarkup,
) (*models.Message, error) {
	if len(req.Media) == 0 {
		return p.api.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:          req.ChatID,
			Text:            req.Text,
			ReplyParameters: reply,
			ReplyMarkup:     markup,
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
			ReplyParameters: reply,
			ReplyMarkup:     markup,
		})

	case comment.MediaVideo:
		return p.api.SendVideo(ctx, &tgbot.SendVideoParams{
			ChatID:          req.ChatID,
			Video:           file,
			Caption:         req.Text,
			ReplyParameters: reply,
			ReplyMarkup:     markup,
		})

	case comment.MediaDocument:
		return p.api.SendDocument(ctx, &tgbot.SendDocumentParams{
			ChatID:          req.ChatID,
			Document:        file,
			Caption:         req.Text,
			ReplyParameters: reply,
			ReplyMarkup:     markup,
		})

	default:
		return nil, fmt.Errorf("unsupported media type %q", media.Type)
	}
}
