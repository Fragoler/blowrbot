package migrations_test

import (
	"database/sql"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/migrations"
)

var namePattern = regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)

// TestProviderAcceptsMigrations parses the embedded set the same way the bot does
// at startup, so a malformed or misnumbered file fails here rather than on deploy.
func TestProviderAcceptsMigrations(t *testing.T) {
	t.Parallel()

	// sql.Open is lazy: the provider only parses the files, it never dials.
	db, err := sql.Open("pgx", "postgres://unused@127.0.0.1:1/unused")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	require.NoError(t, err)

	sources := provider.ListSources()
	require.NotEmpty(t, sources, "the binary must carry its schema")

	versions := make([]int64, 0, len(sources))
	for _, s := range sources {
		versions = append(versions, s.Version)
	}

	assert.True(t, sort.SliceIsSorted(versions, func(i, j int) bool { return versions[i] < versions[j] }))
	assert.Len(t, uniq(versions), len(versions), "migration versions must be unique")
}

// TestFileNames keeps the numbering zero-padded and the set reversible: goose runs
// an Up-only migration happily, but then `migrate:down` silently does nothing.
func TestFileNames(t *testing.T) {
	t.Parallel()

	entries, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, name := range entries {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			match := namePattern.FindStringSubmatch(name)
			require.NotNil(t, match, "want NNNNN_snake_case.sql")

			_, err := strconv.Atoi(match[1])
			require.NoError(t, err)

			body, err := fs.ReadFile(migrations.FS, name)
			require.NoError(t, err)

			assert.Contains(t, string(body), "-- +goose Up")
			assert.Contains(t, string(body), "-- +goose Down")
		})
	}
}

func uniq(versions []int64) []int64 {
	seen := make(map[int64]struct{}, len(versions))
	out := make([]int64, 0, len(versions))

	for _, v := range versions {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	return out
}
