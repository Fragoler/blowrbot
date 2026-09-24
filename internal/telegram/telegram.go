// Package telegram is the transport layer: it turns Telegram updates into calls
// into the core packages and renders the core's results back into Telegram.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	tgbot "github.com/go-telegram/bot"

	"loudbot/internal/comment"
	"loudbot/internal/config"
)

// store is the part of the repository this layer needs directly; everything else
// goes through the core services.
type store interface {
	SaveReport(ctx context.Context, targetType string, targetID, reporterID int64, reason string) error
}

// Bot wires the Telegram client to the comment core.
type Bot struct {
	api      *tgbot.Bot
	cfg      config.Config
	log      *slog.Logger
	comments *comment.Service
	store    store
}

// New builds the Telegram client. The comment service is attached afterwards with
// UseComments, because it needs the publisher that only an existing client provides.
func New(cfg config.Config, st store, log *slog.Logger) (*Bot, error) {
	b := &Bot{
		cfg:   cfg,
		log:   log,
		store: st,
	}

	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(b.handleUpdate),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "channel_post", "callback_query"}),
		tgbot.WithErrorsHandler(func(err error) {
			log.Error("update handling failed", slog.Any("error", err))
		}),
	}

	if cfg.Telegram.Debug {
		opts = append(opts,
			tgbot.WithDebug(),
			tgbot.WithDebugHandler(func(format string, args ...any) {
				log.Debug(fmt.Sprintf(format, args...))
			}),
		)
	}

	api, err := tgbot.New(cfg.Telegram.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create telegram client: %w", err)
	}
	b.api = api

	return b, nil
}

// UseComments attaches the comment core; Run refuses to start without it.
func (b *Bot) UseComments(svc *comment.Service) {
	b.comments = svc
}

// Run starts long polling and blocks until the context is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	if b.comments == nil {
		return errors.New("comment service is not attached")
	}

	me, err := b.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}

	b.log.Info("bot started",
		slog.String("username", me.Username),
		slog.Int64("id", me.ID),
		slog.Int64("channel_id", b.cfg.Telegram.ChannelID),
		slog.Int64("discussion_chat_id", b.cfg.Telegram.DiscussionChatID),
	)

	b.api.Start(ctx)
	b.log.Info("bot stopped")

	return nil
}
