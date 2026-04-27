package redis_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRedisExecutor struct {
	calls   [][]any
	results []any
	errs    []error
}

func (f *fakeRedisExecutor) Do(_ context.Context, args ...any) (any, error) {
	f.calls = append(f.calls, args)
	idx := len(f.calls) - 1
	if idx < len(f.errs) && f.errs[idx] != nil {
		return nil, f.errs[idx]
	}
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return nil, nil
}

func TestListDatabases(t *testing.T) {
	exec := &fakeRedisExecutor{results: []any{"# Keyspace\r\ndb0:keys=2,expires=1,avg_ttl=5\r\ndb3:keys=9,expires=0,avg_ttl=0\r\n"}}

	got, err := listDatabases(context.Background(), exec)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []any{"INFO", "keyspace"}, exec.calls[0])
	assert.Equal(t, 0, got[0].DB)
	assert.Equal(t, int64(2), got[0].Keys)
	assert.Equal(t, 3, got[1].DB)
}

func TestScanKeys(t *testing.T) {
	t.Run("builds scan command with match count and type", func(t *testing.T) {
		exec := &fakeRedisExecutor{results: []any{[]any{"17", []any{"a", "b"}}}}

		got, err := scanKeys(context.Background(), exec, RedisScanRequest{
			Cursor: "0",
			Match:  "user:*",
			Count:  100,
			Type:   "hash",
		})

		require.NoError(t, err)
		assert.Equal(t, []any{"SCAN", "0", "MATCH", "user:*", "COUNT", int64(100), "TYPE", "hash"}, exec.calls[0])
		assert.Equal(t, "17", got.Cursor)
		assert.True(t, got.HasMore)
		assert.Equal(t, []string{"a", "b"}, got.Keys)
	})

	t.Run("exact lookup uses exists and type", func(t *testing.T) {
		exec := &fakeRedisExecutor{results: []any{int64(1), "string"}}

		got, err := scanKeys(context.Background(), exec, RedisScanRequest{
			Match: "session:1",
			Type:  "string",
			Exact: true,
		})

		require.NoError(t, err)
		assert.Equal(t, []any{"EXISTS", "session:1"}, exec.calls[0])
		assert.Equal(t, []any{"TYPE", "session:1"}, exec.calls[1])
		assert.Equal(t, []string{"session:1"}, got.Keys)
		assert.False(t, got.HasMore)
	})
}

func TestGetKeyDetail(t *testing.T) {
	exec := &fakeRedisExecutor{results: []any{
		"hash",
		int64(120),
		int64(42),
		int64(2),
		[]any{"0", []any{"field", "value"}},
	}}

	got, err := getKeyDetail(context.Background(), exec, RedisKeyRequest{Key: "user:1"})

	require.NoError(t, err)
	assert.Equal(t, "hash", got.Type)
	assert.Equal(t, int64(120), got.TTL)
	assert.Equal(t, int64(42), got.Size)
	assert.Equal(t, int64(2), got.Total)
	assert.Equal(t, []RedisHashEntry{{Field: "field", Value: "value"}}, got.Value)
	assert.Equal(t, []any{"TYPE", "user:1"}, exec.calls[0])
	assert.Equal(t, []any{"TTL", "user:1"}, exec.calls[1])
	assert.Equal(t, []any{"MEMORY", "USAGE", "user:1"}, exec.calls[2])
	assert.Equal(t, []any{"HLEN", "user:1"}, exec.calls[3])
	assert.Equal(t, []any{"HSCAN", "user:1", "0", "COUNT", int64(100)}, exec.calls[4])
}
