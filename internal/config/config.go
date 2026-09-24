// Package config загружает настройки бота из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	envToken       = "BOT_TOKEN"
	envChannelID   = "CHANNEL_ID"
	envFirstText   = "FIRST_COMMENT_TEXT"
	envDebug       = "DEBUG"
	defaultComment = "Я первый!"
)

// Config — настройки запуска бота.
type Config struct {
	// Token — токен бота, выданный @BotFather.
	Token string
	// ChannelID — ID канала, посты которого комментируем.
	// Ноль означает «любой канал, привязанный к чату обсуждений».
	ChannelID int64
	// FirstComment — текст, который бот пишет первым комментарием к посту.
	FirstComment string
	// Debug включает подробный лог запросов к Telegram API.
	Debug bool
}

// Load читает конфигурацию из окружения.
func Load() (Config, error) {
	cfg := Config{
		Token:        strings.TrimSpace(os.Getenv(envToken)),
		FirstComment: defaultComment,
	}

	if cfg.Token == "" {
		return Config{}, fmt.Errorf("%s не задан", envToken)
	}

	if raw := strings.TrimSpace(os.Getenv(envChannelID)); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("%s=%q: %w", envChannelID, raw, err)
		}
		cfg.ChannelID = id
	}

	if raw := strings.TrimSpace(os.Getenv(envFirstText)); raw != "" {
		cfg.FirstComment = raw
	}

	if raw := strings.TrimSpace(os.Getenv(envDebug)); raw != "" {
		debug, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s=%q: %w", envDebug, raw, err)
		}
		cfg.Debug = debug
	}

	return cfg, nil
}
