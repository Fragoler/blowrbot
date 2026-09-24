package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"loudbot/internal/config"
	"loudbot/migrations"
)

// Migrate applies the embedded goose migrations. It opens its own database/sql
// handle because goose works over database/sql while the repository uses pgxpool.
// A Postgres session advisory lock keeps concurrent instances from racing.
func Migrate(ctx context.Context, cfg config.Postgres, log *slog.Logger) error {
	db, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn("close migration connection", slog.Any("error", err))
		}
	}()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration locker: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if len(results) == 0 {
		log.Info("database schema is up to date")

		return nil
	}

	for _, r := range results {
		// Not "version": the root logger already carries the build version.
		log.Info("migration applied",
			slog.Int64("migration_version", r.Source.Version),
			slog.String("name", r.Source.Path),
			slog.Duration("duration", r.Duration),
		)
	}

	return nil
}
