package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/config"
)

const validTOML = `
[service]
name = "loudbot"
log_level = "debug"

[telegram]
mode = "polling"
bot_username = "anon_bot"
channel_id = -1001
discussion_chat_id = -1002

[postgres]
host = "db"
port = 5432
user = "loudbot"
dbname = "loudbot"

[moderation]
chat_id = -1003
`

func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestLoad(t *testing.T) {
	t.Setenv(config.EnvBotToken, "secret-token")
	t.Setenv(config.EnvPostgresPassword, "p@ss word")

	cfg, err := config.Load(write(t, validTOML))
	require.NoError(t, err)

	assert.Equal(t, "secret-token", cfg.Telegram.Token)
	assert.Equal(t, config.ModePolling, cfg.Telegram.Mode)
	assert.Equal(t, int64(-1002), cfg.Telegram.DiscussionChatID)
	assert.Equal(t, "p@ss word", cfg.Postgres.Password)
	assert.Equal(t, "disable", cfg.Postgres.SSLMode, "defaults fill what the file omits")
	assert.Equal(t, 3500, cfg.Comments.MaxTextLen)
	assert.Equal(t, ": ", cfg.Comments.NicknameSeparator)
	assert.Equal(t, "postgres://loudbot:p%40ss%20word@db:5432/loudbot?sslmode=disable", cfg.Postgres.DSN())
}

func TestLoadIgnoresSecretsInFile(t *testing.T) {
	t.Setenv(config.EnvBotToken, "from-env")
	t.Setenv(config.EnvPostgresPassword, "")

	cfg, err := config.Load(write(t, validTOML+"\ntoken = \"from-file\"\n"))
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.Telegram.Token)
	assert.Equal(t, "postgres://loudbot@db:5432/loudbot?sslmode=disable", cfg.Postgres.DSN())
}

func TestLoadUsesConfigPathEnv(t *testing.T) {
	t.Setenv(config.EnvBotToken, "secret-token")
	t.Setenv(config.EnvConfigPath, write(t, validTOML))

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "anon_bot", cfg.Telegram.BotUsername)
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		token string
		body  string
		want  string
	}{
		{name: "missing token", body: validTOML, want: config.EnvBotToken},
		{name: "broken toml", token: "t", body: "[telegram", want: "parse config"},
		{name: "unknown mode", token: "t", body: strings.Replace(validTOML, `mode = "polling"`, `mode = "carrier-pigeon"`, 1), want: "telegram.mode"},
		{name: "bad log level", token: "t", body: "[service]\nlog_level = \"loud\"\n", want: "service.log_level"},
		{name: "missing ids", token: "t", body: "[service]\nname = \"x\"\n", want: "telegram.channel_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(config.EnvBotToken, tc.token)

			_, err := config.Load(write(t, tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv(config.EnvBotToken, "t")

	_, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLogLevel(t *testing.T) {
	t.Setenv(config.EnvBotToken, "t")

	cfg, err := config.Load(write(t, validTOML))
	require.NoError(t, err)
	assert.Equal(t, "DEBUG", cfg.LogLevel().String())
}

func TestDraftTTL(t *testing.T) {
	t.Setenv(config.EnvBotToken, "t")

	cfg, err := config.Load(write(t, validTOML))
	require.NoError(t, err)

	ttl, err := cfg.Comments.TTL()
	require.NoError(t, err)
	assert.Equal(t, time.Hour, ttl, "the default applies when the file omits draft_ttl")

	cfg, err = config.Load(write(t, validTOML+"\n[comments]\ndraft_ttl = \"30m\"\n"))
	require.NoError(t, err)

	ttl, err = cfg.Comments.TTL()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, ttl)

	for _, bad := range []string{`draft_ttl = "soon"`, `draft_ttl = "-5m"`, `draft_ttl = ""`} {
		_, err := config.Load(write(t, validTOML+"\n[comments]\n"+bad+"\n"))
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "comments.draft_ttl")
	}
}

// TestExampleConfigIsValid keeps config.example.toml in step with the schema.
func TestExampleConfigIsValid(t *testing.T) {
	t.Setenv(config.EnvBotToken, "t")

	cfg, err := config.Load(filepath.Join("..", "..", "config.example.toml"))
	require.NoError(t, err)
	assert.Equal(t, config.ModePolling, cfg.Telegram.Mode)

	_, err = cfg.Comments.TTL()
	require.NoError(t, err)
}
