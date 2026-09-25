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

[[nicknames]]
label = "Лис"

[[nicknames]]
label = "Сова"
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
	assert.Equal(t, []config.Nickname{{Label: "Лис"}, {Label: "Сова"}}, cfg.Nicknames)
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
		{name: "no nicknames", token: "t", body: strings.Split(validTOML, "[[nicknames]]")[0], want: "at least one [[nicknames]]"},
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

func TestNicknameValidation(t *testing.T) {
	base := strings.Split(validTOML, "[[nicknames]]")[0]

	cases := []struct {
		name string
		list string
		want string
	}{
		{
			name: "blank label",
			list: "[[nicknames]]\nlabel = \"\"\n",
			want: "nicknames[0].label is empty",
		},
		{
			name: "untrimmed label",
			list: "[[nicknames]]\nlabel = \" Лис \"\n",
			want: "leading or trailing spaces",
		},
		{
			name: "duplicate label",
			list: "[[nicknames]]\nlabel = \"Лис\"\n\n[[nicknames]]\nlabel = \"Лис\"\n",
			want: "nicknames[1].label=\"Лис\" is a duplicate",
		},
		{
			// A label is carried inside callback_data, which Telegram caps at 64 bytes.
			name: "label too long for a button",
			list: "[[nicknames]]\nlabel = \"" + strings.Repeat("я", 40) + "\"\n",
			want: "is too long",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(config.EnvBotToken, "t")

			_, err := config.Load(write(t, base+tc.list))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestNicknameAtCallbackLimitIsAccepted(t *testing.T) {
	t.Setenv(config.EnvBotToken, "t")

	// "nick:" is 5 bytes, so 59 bytes of label is the longest that still fits.
	label := strings.Repeat("я", 29) + "a"
	require.Len(t, []byte(label), 59)

	base := strings.Split(validTOML, "[[nicknames]]")[0]
	cfg, err := config.Load(write(t, base+"[[nicknames]]\nlabel = \""+label+"\"\n"))
	require.NoError(t, err)
	assert.Equal(t, label, cfg.Nicknames[0].Label)
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
