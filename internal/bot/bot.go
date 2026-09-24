// Package anonbot содержит телеграм-бота, который комментирует посты канала.
package bot

import (
	"context"
	"fmt"
	"log/slog"

	tgbot "github.com/go-telegram/bot"

	"loudbot/internal/config"
)

// Bot — обёртка над клиентом Telegram Bot API с обработчиками апдейтов.
type Bot struct {
	api *tgbot.Bot
	cfg config.Config
	log *slog.Logger
}

// New собирает бота по конфигурации.
func New(cfg config.Config, log *slog.Logger) (*Bot, error) {
	b := &Bot{
		cfg: cfg,
		log: log,
	}

	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(b.handleUpdate),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "channel_post"}),
		tgbot.WithErrorsHandler(func(err error) {
			log.Error("ошибка обработки апдейта", slog.Any("error", err))
		}),
	}

	if cfg.Debug {
		opts = append(opts,
			tgbot.WithDebug(),
			tgbot.WithDebugHandler(func(format string, args ...any) {
				log.Debug(fmt.Sprintf(format, args...))
			}),
		)
	}

	api, err := tgbot.New(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать клиента Telegram: %w", err)
	}
	b.api = api

	return b, nil
}

// Run запускает long polling и блокируется до отмены контекста.
func (b *Bot) Run(ctx context.Context) error {
	me, err := b.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}

	b.log.Info("бот запущен",
		slog.String("username", me.Username),
		slog.Int64("id", me.ID),
		slog.Int64("channel_id", b.cfg.ChannelID),
	)

	b.api.Start(ctx)
	b.log.Info("бот остановлен")

	return nil
}
