package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"loudbot/internal/bot"
	"loudbot/internal/config"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("бот завершился с ошибкой", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("конфигурация: %w", err)
	}

	bot, err := bot.New(cfg, log)
	if err != nil {
		return fmt.Errorf("инициализация бота: %w", err)
	}

	return bot.Run(ctx)
}

func logLevel() slog.Level {
	if os.Getenv("DEBUG") == "true" {
		return slog.LevelDebug
	}

	return slog.LevelInfo
}
