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
	questions                   []extension.ReviewQuestion
	calls                       [][]extension.ReviewItem
	reply                       func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error)
}

func (f *fakeExtensionReviewer) Identity() (string, string, string) {
	return f.name, f.version, f.settingsHash
}
func (f *fakeExtensionReviewer) Limits() (int, int)                    { return f.maxItems, f.maxCommandLen }
func (f *fakeExtensionReviewer) Questions() []extension.ReviewQuestion { return f.questions }
func (f *fakeExtensionReviewer) Review(_ context.Context, items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
	cp := append([]extension.ReviewItem(nil), items...)
	f.calls = append(f.calls, cp)
	return f.reply(items)
}

var destructiveQuestion = []extension.ReviewQuestion{{
	ID:   "destructive",
	I18n: extension.I18nNames{Name: map[string]string{"en": "Destroys data", "zh-CN": "破坏数据"}},
}}

// 审核扩展的 error 是"没能得到判断"，宿主按审核失败处理，也不能缓存——否则一次限流会让同一条命令 24 小时内都审核失败。
func TestExtensionReviewerErrorIsFailureAndNotCached(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 100, questions: destructiveQuestion,
		reply: func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			return []extension.ReviewCommandResult{{Outcome: extension.ReviewOutcomeError, Reason: "rate_limited"}}, nil
		},
	}
	s, cache := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Equal(t, OutcomeFail, r.Outcome)
	assert.Equal(t, "rate_limited", r.Reason)
	assert.Empty(t, cache.m, "审核失败不进缓存")

	s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Len(t, rev.calls, 2)
}

// 未通过的题目带上扩展声明的各语言名称，审批提示和审计据此显示。
func TestExtensionReviewerRejectCarriesQuestionNames(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 100, questions: destructiveQuestion,
		reply: func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			return []extension.ReviewCommandResult{{Outcome: extension.ReviewOutcomeReject, Failed: []string{"destructive"}}}, nil
		},
	}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "rm -rf /data"})
	assert.Equal(t, OutcomeReject, r.Outcome)
	assert.Equal(t, map[string]map[string]string{"destructive": {"en": "Destroys data", "zh-CN": "破坏数据"}}, r.FailedNames)
}

func TestExtensionReviewerSplitsBatchesAndRejectsOverlong(t *testing.T) {
	rev := &fakeExtensionReviewer{
		name: "rev", version: "1.2.0", settingsHash: "abc",
		maxItems: 2, maxCommandLen: 4, questions: destructiveQuestion,
		reply: func(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
			out := make([]extension.ReviewCommandResult, len(items))
			for i := range items {
				out[i] = extension.ReviewCommandResult{Outcome: extension.ReviewOutcomePass}
			}
			return out, nil
		},
	}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

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
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

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
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

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

// 选中了审核器但这次解析失败（未加载、摘要不符）时整批失败，不退回内置审核。
func TestSelectedReviewerUnavailableFailsInsteadOfBuiltin(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{}}
	s, _ := newTestService(ev, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) {
		return nil, ErrReviewerUnavailable
	}

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Equal(t, OutcomeFail, r.Outcome)
	assert.Equal(t, ReasonReviewerUnavailable, r.Reason)
	assert.Equal(t, 0, ev.calls, "选中的审核器不可用时不能改问内置模型")
}

func TestNoExtensionReviewerKeepsBuiltin(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{}}
	s, _ := newTestService(ev, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return nil, nil }

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
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Equal(t, OutcomeFail, r.Outcome)
	assert.Equal(t, ReasonTimeout, r.Reason)
}

func passAll(items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
	out := make([]extension.ReviewCommandResult, len(items))
	for i := range out {
		out[i] = extension.ReviewCommandResult{Outcome: extension.ReviewOutcomePass}
	}
	return out, nil
}

// 审核扩展和内置审核看到的是同一份命令：解码后执行的脚本先展开，扩展不能只看到编码后的外壳。
func TestExtensionReviewerSeesDecodedScript(t *testing.T) {
	script := "cd /root/app\ndocker compose up -d"
	rev := &fakeExtensionReviewer{name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 1000, reply: passAll}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Syntax: SyntaxShell, Command: pipeDecodeExec(script, "bash")})

	assert.Equal(t, OutcomePass, r.Outcome)
	require.Len(t, rev.calls, 1)
	assert.Equal(t, script, rev.calls[0][0].Command)
}

// 解不开要执行的内容时不交给扩展，按 undecodable 失败。
func TestExtensionReviewerNotCalledForUndecodableExec(t *testing.T) {
	rev := &fakeExtensionReviewer{name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 1000, reply: passAll}
	s, _ := newTestService(&fakeEvaluator{}, enabledConfig())
	s.extension = func(context.Context) (ExtensionReviewer, error) { return rev, nil }

	r := s.Review(context.Background(), Input{AssetType: "ssh", Syntax: SyntaxShell, Command: "echo '!!!' | base64 -d | bash"})

	assert.Equal(t, OutcomeFail, r.Outcome)
	assert.Equal(t, ReasonUndecodable, r.Reason)
	assert.Empty(t, rev.calls)
}

// 内置审核的 API key 解不开不影响审核扩展：扩展照常审核，超时用设置里的值。
func TestExtensionReviewerIgnoresUnreadableBuiltinKey(t *testing.T) {
	rev := &fakeExtensionReviewer{name: "rev", version: "1.0.0", maxItems: 8, maxCommandLen: 1000, reply: passAll}
	cfg := enabledConfig()
	cfg.APIKey = ""
	s := New(func() (Config, error) { return cfg, errors.New("decrypt: cipher: message authentication failed") },
		&memCache{m: map[string]Result{}}, func(string, string) Evaluator { return &fakeEvaluator{} },
		func(context.Context) (ExtensionReviewer, error) { return rev, nil })

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})

	assert.Equal(t, OutcomePass, r.Outcome)
	assert.Len(t, rev.calls, 1)
}
