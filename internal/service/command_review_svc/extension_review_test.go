package command_review_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opskat/opskat/pkg/extension"
)

type fakeExtensionReviewer struct {
	name, version, settingsHash string
	maxItems, maxCommandLen     int
	questions                   []string
	calls                       [][]extension.ReviewItem
	reply                       func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error)
}

func (f *fakeExtensionReviewer) Identity() (string, string, string) {
	return f.name, f.version, f.settingsHash
}
func (f *fakeExtensionReviewer) Limits() (int, int) { return f.maxItems, f.maxCommandLen }
func (f *fakeExtensionReviewer) Questions() []string { return f.questions }
func (f *fakeExtensionReviewer) Review(_ context.Context, items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
	cp := append([]extension.ReviewItem(nil), items...)
	f.calls = append(f.calls, cp)
	return f.reply(items)
}

func TestExtensionReviewerSplitsBatchesAndRejectsOverlong(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.2.0", settingsHash: "abc",
		maxItems: 2, maxCommandLen: 4, questions: []string{"destructive"},
		reply: func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			out := make([]extension.ReviewCommandResult, len(items))
			for i := range items {
				out[i] = extension.ReviewCommandResult{Outcome: extension.ReviewOutcomePass}
			}
			return out, nil
		},
	}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func() ExtensionReviewer { return rev }

	got := s.ReviewBatch(context.Background(), []Input{
		{AssetType: "ssh", Command: "ls", Syntax: SyntaxText},
		{AssetType: "ssh", Command: "pwd", Syntax: SyntaxText},
		{AssetType: "ssh", Command: "who", Syntax: SyntaxText},
		{AssetType: "ssh", Command: "too-long", Syntax: SyntaxText},
	})

	require.Len(t, rev.calls, 2, "maxItems=2，三条可审命令要拆成两次调用")
	assert.Len(t, rev.calls[0], 2)
	assert.Len(t, rev.calls[1], 1)
	assert.Equal(t, "ls", rev.calls[0][0].Command)
	assert.Equal(t, OutcomePass, got[0].Outcome)
	assert.Equal(t, OutcomePass, got[2].Outcome)
	assert.Equal(t, OutcomeFail, got[3].Outcome)
	assert.Equal(t, ReasonTooLong, got[3].Reason)
	assert.Equal(t, "rev", got[0].Extension)
	assert.Equal(t, "1.2.0", got[0].Version)
}

func TestExtensionReviewerInvalidResultFailsTheBatch(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 100,
		reply: func([]extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			return nil, errors.New("review invalid_result: got 1 results for 2 commands")
		},
	}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func() ExtensionReviewer { return rev }

	got := s.ReviewBatch(context.Background(), []Input{
		{AssetType: "ssh", Command: "ls"},
		{AssetType: "ssh", Command: "pwd"},
	})
	assert.Equal(t, OutcomeFail, got[0].Outcome)
	assert.Equal(t, OutcomeFail, got[1].Outcome)
	assert.Equal(t, ReasonInvalidResult, got[0].Reason)
}

func TestExtensionReviewerCacheKeyIncludesIdentity(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.0.0", settingsHash: "h1",
		maxItems: 8, maxCommandLen: 100,
		reply: func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			out := make([]extension.ReviewCommandResult, len(items))
			for i := range out {
				out[i] = extension.ReviewCommandResult{Outcome: extension.ReviewOutcomePass}
			}
			return out, nil
		},
	}
	s, cache := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func() ExtensionReviewer { return rev }

	first := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.False(t, first.Cached)
	assert.Equal(t, 1, len(rev.calls))

	second := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.True(t, second.Cached)
	assert.Equal(t, 1, len(rev.calls))

	rev.settingsHash = "h2"
	third := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.False(t, third.Cached, "设置哈希变了不能命中旧缓存")
	assert.Equal(t, 2, len(rev.calls))
	assert.NotEmpty(t, cache.m)
}

func TestNoExtensionReviewerKeepsBuiltin(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{}}
	s, _ := newTestService(ev, enabledConfig())
	s.extension = func() ExtensionReviewer { return nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Equal(t, OutcomePass, r.Outcome)
	assert.Equal(t, 1, ev.calls)
	assert.Empty(t, r.Extension)
}

func TestExtensionReviewerTimeout(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 100,
		reply: func([]extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			return nil, context.DeadlineExceeded
		},
	}
	cfg := enabledConfig()
	cfg.Timeout = 5 * time.Millisecond
	s, _ := newTestService(&fakeEvaluator{}, cfg)
	s.extension = func() ExtensionReviewer { return rev }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Equal(t, OutcomeFail, r.Outcome)
	assert.Equal(t, ReasonTimeout, r.Reason)
}
