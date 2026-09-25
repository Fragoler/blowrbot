package chatlog_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"loudbot/internal/chatlog"
)

var errBoom = errors.New("boom")

const userID = int64(777)

// fakeRepo is an in-memory transcript.
type fakeRepo struct {
	stored map[int64][]int

	rememberErr error
	messagesErr error
	forgetErr   error
}

func newRepo(ids ...int) *fakeRepo {
	r := &fakeRepo{stored: map[int64][]int{}}
	if len(ids) > 0 {
		r.stored[userID] = ids
	}

	return r
}

func (r *fakeRepo) Remember(_ context.Context, id int64, messageIDs ...int) error {
	if r.rememberErr != nil {
		return r.rememberErr
	}

	r.stored[id] = append(r.stored[id], messageIDs...)

	return nil
}

func (r *fakeRepo) Messages(_ context.Context, id int64) ([]int, error) {
	if r.messagesErr != nil {
		return nil, r.messagesErr
	}

	return r.stored[id], nil
}

func (r *fakeRepo) Forget(_ context.Context, id int64, messageIDs ...int) error {
	if r.forgetErr != nil {
		return r.forgetErr
	}

	gone := make(map[int]struct{}, len(messageIDs))
	for _, m := range messageIDs {
		gone[m] = struct{}{}
	}

	kept := r.stored[id][:0]
	for _, m := range r.stored[id] {
		if _, ok := gone[m]; !ok {
			kept = append(kept, m)
		}
	}
	r.stored[id] = kept

	return nil
}

// fakeDeleter records the batches Telegram was asked to remove.
type fakeDeleter struct {
	batches [][]int
	err     error
	failOn  int // 1-based index of the batch that fails; 0 means none
}

func (d *fakeDeleter) DeleteMessages(_ context.Context, _ int64, messageIDs []int) error {
	d.batches = append(d.batches, append([]int(nil), messageIDs...))

	if d.err != nil {
		return d.err
	}

	if d.failOn == len(d.batches) {
		return errBoom
	}

	return nil
}

func newLog(repo *fakeRepo, del *fakeDeleter) *chatlog.Log {
	return chatlog.New(repo, del, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRecord(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	require.NoError(t, newLog(repo, &fakeDeleter{}).Record(context.Background(), userID, 5, 6))
	assert.Equal(t, []int{5, 6}, repo.stored[userID])
}

func TestRecordIgnoresNothingToStore(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	log := newLog(repo, &fakeDeleter{})
	ctx := context.Background()

	require.NoError(t, log.Record(ctx, userID))
	require.NoError(t, log.Record(ctx, userID, 0), "a zero id is not a message")
	assert.Empty(t, repo.stored[userID])
}

func TestWipeDeletesEverythingAndForgetsIt(t *testing.T) {
	t.Parallel()

	repo := newRepo(7, 5, 6)
	del := &fakeDeleter{}

	require.NoError(t, newLog(repo, del).Wipe(context.Background(), userID))

	require.Len(t, del.batches, 1)
	assert.Equal(t, []int{5, 6, 7}, del.batches[0], "a chat disappears oldest first")
	assert.Empty(t, repo.stored[userID])
}

func TestWipeIncludesExtraIDs(t *testing.T) {
	t.Parallel()

	repo := newRepo(5)
	del := &fakeDeleter{}

	// The "/start" a deep link makes the user send is not recorded yet.
	require.NoError(t, newLog(repo, del).Wipe(context.Background(), userID, 9))

	require.Len(t, del.batches, 1)
	assert.Equal(t, []int{5, 9}, del.batches[0])
}

func TestWipeDeduplicates(t *testing.T) {
	t.Parallel()

	repo := newRepo(5, 5, 6)
	del := &fakeDeleter{}

	require.NoError(t, newLog(repo, del).Wipe(context.Background(), userID, 6))
	require.Len(t, del.batches, 1)
	assert.Equal(t, []int{5, 6}, del.batches[0])
}

func TestWipeBatchesAtTelegramsLimit(t *testing.T) {
	t.Parallel()

	ids := make([]int, 0, 250)
	for i := 1; i <= 250; i++ {
		ids = append(ids, i)
	}

	repo := newRepo(ids...)
	del := &fakeDeleter{}

	require.NoError(t, newLog(repo, del).Wipe(context.Background(), userID))

	require.Len(t, del.batches, 3, "deleteMessages takes at most 100 ids per call")
	assert.Len(t, del.batches[0], 100)
	assert.Len(t, del.batches[1], 100)
	assert.Len(t, del.batches[2], 50)
	assert.Empty(t, repo.stored[userID])
}

func TestWipeKeepsWhatTelegramRefused(t *testing.T) {
	t.Parallel()

	ids := make([]int, 0, 150)
	for i := 1; i <= 150; i++ {
		ids = append(ids, i)
	}

	repo := newRepo(ids...)
	del := &fakeDeleter{failOn: 1}

	err := newLog(repo, del).Wipe(context.Background(), userID)
	require.ErrorIs(t, err, errBoom)

	// The failed batch stays on the books so the next wipe can try again; the one
	// that went through is forgotten.
	assert.Len(t, repo.stored[userID], 100)
	assert.Equal(t, 1, repo.stored[userID][0])
	assert.Len(t, del.batches, 2, "a failed batch does not stop the rest")
}

func TestWipeOnEmptyChat(t *testing.T) {
	t.Parallel()

	del := &fakeDeleter{}
	require.NoError(t, newLog(newRepo(), del).Wipe(context.Background(), userID))
	assert.Empty(t, del.batches, "nothing to delete means no call to Telegram")
}

func TestWipeReportsRepositoryFailure(t *testing.T) {
	t.Parallel()

	repo := newRepo(5)
	repo.messagesErr = errBoom

	require.ErrorIs(t, newLog(repo, &fakeDeleter{}).Wipe(context.Background(), userID), errBoom)
}

func TestForget(t *testing.T) {
	t.Parallel()

	repo := newRepo(5, 6, 7)
	require.NoError(t, newLog(repo, &fakeDeleter{}).Forget(context.Background(), userID, 6))
	assert.Equal(t, []int{5, 7}, repo.stored[userID])
}
