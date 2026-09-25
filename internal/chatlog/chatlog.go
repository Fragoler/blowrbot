// Package chatlog remembers what the bot and one person have said to each other
// in private, so that starting a new flow can clear the screen first.
//
// It exists because the Bot API offers no way to read a chat's history: a bot can
// only delete messages whose ids it kept. Telegram also refuses to delete
// anything older than 48 hours, silently skipping it, so an old transcript may
// survive in part — the wipe is best-effort by construction.
package chatlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

// batchSize is Telegram's limit for one deleteMessages call.
const batchSize = 100

// Repository stores the message ids of one private chat.
type Repository interface {
	Remember(ctx context.Context, userID int64, messageIDs ...int) error
	Messages(ctx context.Context, userID int64) ([]int, error)
	Forget(ctx context.Context, userID int64, messageIDs ...int) error
}

// Deleter removes messages from a private chat, skipping what it cannot delete.
type Deleter interface {
	DeleteMessages(ctx context.Context, chatID int64, messageIDs []int) error
}

// Log is the private-chat transcript of every user the bot talks to.
type Log struct {
	repo Repository
	del  Deleter
	log  *slog.Logger
}

func New(repo Repository, del Deleter, log *slog.Logger) *Log {
	return &Log{repo: repo, del: del, log: log}
}

// Record notes messages that are now on screen in the private chat with userID.
func (l *Log) Record(ctx context.Context, userID int64, messageIDs ...int) error {
	ids := clean(messageIDs)
	if len(ids) == 0 {
		return nil
	}

	if err := l.repo.Remember(ctx, userID, ids...); err != nil {
		return fmt.Errorf("remember messages: %w", err)
	}

	return nil
}

// Forget drops messages that are already gone from the chat, so a later wipe
// does not waste a delete call on them.
func (l *Log) Forget(ctx context.Context, userID int64, messageIDs ...int) error {
	ids := clean(messageIDs)
	if len(ids) == 0 {
		return nil
	}

	if err := l.repo.Forget(ctx, userID, ids...); err != nil {
		return fmt.Errorf("forget messages: %w", err)
	}

	return nil
}

// Wipe clears the private chat, including any extra ids not recorded yet — the
// "/start" a deep link makes the user send, for instance. Only the batches that
// Telegram actually accepted are forgotten, so a transport failure leaves them
// to be retried by the next wipe.
func (l *Log) Wipe(ctx context.Context, userID int64, extra ...int) error {
	known, err := l.repo.Messages(ctx, userID)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}

	ids := clean(append(known, extra...))
	if len(ids) == 0 {
		return nil
	}

	var errs []error

	for chunk := range slices.Chunk(ids, batchSize) {
		if err := l.del.DeleteMessages(ctx, userID, chunk); err != nil {
			errs = append(errs, fmt.Errorf("delete %d messages: %w", len(chunk), err))

			continue
		}

		if err := l.repo.Forget(ctx, userID, chunk...); err != nil {
			errs = append(errs, fmt.Errorf("forget %d messages: %w", len(chunk), err))
		}
	}

	return errors.Join(errs...)
}

// clean drops zero ids and duplicates, and orders what is left oldest first, so
// a chat disappears from the top down rather than in an arbitrary order.
func clean(ids []int) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, id)
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}
