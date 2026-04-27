package redis_svc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandHistory(t *testing.T) {
	h := NewCommandHistory(2)

	h.Add(CommandHistoryEntry{AssetID: 1, DB: 0, Command: "GET a", Timestamp: 1})
	h.Add(CommandHistoryEntry{AssetID: 1, DB: 0, Command: "GET b", Timestamp: 2})
	h.Add(CommandHistoryEntry{AssetID: 2, DB: 0, Command: "GET c", Timestamp: 3})

	all := h.List(0, 0)
	assert.Len(t, all, 2)
	assert.Equal(t, "GET c", all[0].Command)
	assert.Equal(t, "GET b", all[1].Command)

	assetOnly := h.List(1, 10)
	assert.Len(t, assetOnly, 1)
	assert.Equal(t, "GET b", assetOnly[0].Command)
}

func TestFormatCommandForHistory(t *testing.T) {
	got := formatCommandForHistory([]any{"SET", "my key", "value with spaces"})

	assert.Equal(t, `SET "my key" "value with spaces"`, got)
}
