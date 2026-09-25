// Package config loads service settings from a TOML file and secrets from the environment.
// Secrets never come from the file: the TOML fields for them are explicitly skipped.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"loudbot/internal/comment"
)

const (
	// EnvConfigPath overrides the default config file location.
	EnvConfigPath = "CONFIG_PATH"
	// EnvBotToken holds the Telegram bot token.
	EnvBotToken = "BOT_TOKEN"
	// EnvPostgresPassword holds the Postgres password.
	//nolint:gosec // G101: this is the name of an environment variable, not a credential.
	EnvPostgresPassword = "POSTGRES_PASSWORD"

	defaultPath = "config.toml"
)

// Mode is the way the bot receives updates from Telegram.
type Mode string

const (
	ModePolling Mode = "polling"
	ModeWebhook Mode = "webhook"
)

type Config struct {
	Service    Service    `toml:"service"`
	Telegram   Telegram   `toml:"telegram"`
	Postgres   Postgres   `toml:"postgres"`
	Moderation Moderation `toml:"moderation"`
	Comments   Comments   `toml:"comments"`
	Messages   Messages   `toml:"messages"`
	// Nicknames is the curated mask list. It lives here rather than in the
	// database so that editing it is a config change and a restart, no migration.
	Nicknames []Nickname `toml:"nicknames"`
}

// Nickname is one entry of the mask list. Label is the identity: it is stored on
// every published comment, so renaming one here does not rewrite old messages.
type Nickname struct {
	Label string `toml:"label"`
}

type Service struct {
	Name     string `toml:"name"`
	LogLevel string `toml:"log_level"`
}

type Telegram struct {
	Mode        Mode   `toml:"mode"`
	BotUsername string `toml:"bot_username"`
	Debug       bool   `toml:"debug"`

	// ChannelID is the channel the bot posts to and watches.
	ChannelID int64 `toml:"channel_id"`
	// DiscussionChatID is the linked discussion group where comments are published.
	DiscussionChatID int64 `toml:"discussion_chat_id"`

	Token string `toml:"-"`
}

type Postgres struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	DBName   string `toml:"dbname"`
	SSLMode  string `toml:"sslmode"`
	MaxConns int32  `toml:"max_conns"`

	Password string `toml:"-"`
}

type Moderation struct {
	ChatID int64 `toml:"chat_id"`
}

type Comments struct {
	// MaxTextLen caps the comment body before the nickname prefix is added.
	MaxTextLen int `toml:"max_text_len"`
	// DraftTTL bounds how long a deep-link tap stays valid, e.g. "1h" or "30m".
	DraftTTL       string `toml:"draft_ttl"`
	AnswerLinkText string `toml:"answer_link_text"`
}

// TTL parses DraftTTL; Validate reports a malformed value separately.
func (c Comments) TTL() (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(c.DraftTTL))
	if err != nil {
		return 0, fmt.Errorf("comments.draft_ttl=%q: %w", c.DraftTTL, err)
	}

	if d <= 0 {
		return 0, fmt.Errorf("comments.draft_ttl=%q must be positive", c.DraftTTL)
	}

	return d, nil
}

// Load reads the TOML file at path (falling back to CONFIG_PATH, then config.toml),
// applies defaults, overlays secrets from the environment and validates the result.
func Load(path string) (Config, error) {
	if path == "" {
		path = strings.TrimSpace(os.Getenv(EnvConfigPath))
	}
	if path == "" {
		path = defaultPath
	}

	// G703: the path comes from the operator's own -config flag or CONFIG_PATH,
	// never from a user of the bot, so there is no untrusted input to traverse with.
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := defaults()
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	applySecrets(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %q: %w", path, err)
	}

	return cfg, nil
}

func defaults() Config {
	return Config{
		Service: Service{
			Name:     "loudbot",
			LogLevel: "info",
		},
		Telegram: Telegram{
			Mode: ModePolling,
		},
		Postgres: Postgres{
			Host:     "localhost",
			Port:     5432,
			SSLMode:  "disable",
			MaxConns: 10,
		},
		Messages: DefaultMessages(),
		Comments: Comments{
			MaxTextLen: 3500,
			DraftTTL:   "1h",
		},
	}
}

func applySecrets(cfg *Config) {
	cfg.Telegram.Token = strings.TrimSpace(os.Getenv(EnvBotToken))
	cfg.Postgres.Password = os.Getenv(EnvPostgresPassword)
}

func (c Config) Validate() error {
	var errs []error

	if c.Telegram.Token == "" {
		errs = append(errs, fmt.Errorf("%s is not set", EnvBotToken))
	}

	if strings.TrimSpace(c.Telegram.BotUsername) == "" {
		errs = append(errs, errors.New("telegram.bot_username is required for comment deep links"))
	}

	switch c.Telegram.Mode {
	case ModePolling, ModeWebhook:
	default:
		errs = append(errs, fmt.Errorf("telegram.mode=%q: want %q or %q", c.Telegram.Mode, ModePolling, ModeWebhook))
	}

	if c.Telegram.ChannelID == 0 {
		errs = append(errs, errors.New("telegram.channel_id is required"))
	}

	if c.Telegram.DiscussionChatID == 0 {
		errs = append(errs, errors.New("telegram.discussion_chat_id is required"))
	}

	if c.Moderation.ChatID == 0 {
		errs = append(errs, errors.New("moderation.chat_id is required"))
	}

	if strings.TrimSpace(c.Postgres.Host) == "" {
		errs = append(errs, errors.New("postgres.host is required"))
	}

	if strings.TrimSpace(c.Postgres.DBName) == "" {
		errs = append(errs, errors.New("postgres.dbname is required"))
	}

	if strings.TrimSpace(c.Postgres.User) == "" {
		errs = append(errs, errors.New("postgres.user is required"))
	}

	if c.Postgres.Port <= 0 || c.Postgres.Port > 65535 {
		errs = append(errs, fmt.Errorf("postgres.port=%d is out of range", c.Postgres.Port))
	}

	if c.Comments.MaxTextLen <= 0 {
		errs = append(errs, fmt.Errorf("comments.max_text_len=%d must be positive", c.Comments.MaxTextLen))
	}

	if _, err := c.Comments.TTL(); err != nil {
		errs = append(errs, err)
	}

	errs = append(errs, c.validateNicknames()...)
	errs = append(errs, c.Messages.validate()...)

	if _, err := parseLevel(c.Service.LogLevel); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateNicknames rejects a list the bot could not actually offer: an empty one,
// blank or duplicated labels, or a label too long to survive Telegram's 64-byte
// callback_data limit once it is put on a button.
func (c Config) validateNicknames() []error {
	if len(c.Nicknames) == 0 {
		return []error{errors.New("at least one [[nicknames]] entry is required")}
	}

	var (
		errs []error
		seen = make(map[string]struct{}, len(c.Nicknames))
	)

	for i, n := range c.Nicknames {
		label := strings.TrimSpace(n.Label)

		switch {
		case label == "":
			errs = append(errs, fmt.Errorf("nicknames[%d].label is empty", i))

			continue
		case label != n.Label:
			errs = append(errs, fmt.Errorf("nicknames[%d].label=%q has leading or trailing spaces", i, n.Label))
		}

		if _, dup := seen[label]; dup {
			errs = append(errs, fmt.Errorf("nicknames[%d].label=%q is a duplicate", i, label))
		}
		seen[label] = struct{}{}

		if !comment.NicknameCallbackFits(label) {
			errs = append(errs, fmt.Errorf(
				"nicknames[%d].label=%q is too long: %d bytes, and a button carries it within %d",
				i, label, len(label), comment.MaxCallbackLen,
			))
		}
	}

	return errs
}

// LogLevel maps service.log_level onto slog; an unparsable value falls back to info
// so that logging never blocks startup (Validate reports it separately).
func (c Config) LogLevel() slog.Level {
	level, err := parseLevel(c.Service.LogLevel)
	if err != nil {
		return slog.LevelInfo
	}

	return level
}

func parseLevel(raw string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(raw))); err != nil {
		return 0, fmt.Errorf("service.log_level=%q: %w", raw, err)
	}

	return level, nil
}

// DSN builds a connection string for both pgxpool and database/sql. Pool sizing is
// applied in code rather than here, because database/sql rejects pool-only options.
func (p Postgres) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:   "/" + p.DBName,
	}

	if p.Password == "" {
		u.User = url.User(p.User)
	} else {
		u.User = url.UserPassword(p.User, p.Password)
	}

	q := url.Values{}
	if p.SSLMode != "" {
		q.Set("sslmode", p.SSLMode)
	}
	u.RawQuery = q.Encode()

	return u.String()
}
