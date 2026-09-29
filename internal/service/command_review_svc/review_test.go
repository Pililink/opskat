package command_review_svc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opskat/opskat/internal/pkg/typesafe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEvaluator 记录收到的请求，并按题目 ID 返回预设的"是"概率。
type fakeEvaluator struct {
	nouls map[string]float64
	err   error
	delay time.Duration
	calls int
	last  typesafe.Request
}

func (f *fakeEvaluator) Evaluate(ctx context.Context, req typesafe.Request) (*typesafe.Response, error) {
	f.calls++
	f.last = req
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	answers := map[string]typesafe.Answer{}
	for id := range req.Questions {
		answers[id] = typesafe.Answer{Type: typesafe.QuestionNoul, Noul: f.nouls[id]}
	}
	return &typesafe.Response{Model: "jev-1.13.0", Answers: answers}, nil
}

// memCache 是内存版缓存。
type memCache struct{ m map[string]Result }

func (c *memCache) Get(_ context.Context, key string) (*Result, error) {
	r, ok := c.m[key]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (c *memCache) Put(_ context.Context, key string, r Result, _ time.Duration) error {
	c.m[key] = r
	return nil
}

func newTestService(ev *fakeEvaluator, cfg Config) (*service, *memCache) {
	cache := &memCache{m: map[string]Result{}}
	s := New(func() Config { return cfg }, cache, func(string) Evaluator { return ev }).(*service)
	return s, cache
}

func enabledConfig() Config {
	return Config{APIKey: "k", Model: "jev-1.13.0", Threshold: 0.2, Timeout: time.Second, MaxCommandLen: 4000}
}

func TestReviewPassesWhenEveryRiskIsBelowThreshold(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{QuestionDestructive: 0.01, QuestionDisruptive: 0.05, QuestionRemoteCode: 0.0}}
	s, _ := newTestService(ev, enabledConfig())

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "mkdir -p /data/app"})

	assert.Equal(t, OutcomePass, r.Outcome)
	assert.Equal(t, "jev-1.13.0", r.Model)
	assert.Len(t, r.Scores, 3)
}

func TestReviewRejectsWhenAnyRiskReachesThreshold(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{QuestionDestructive: 0.97}}
	s, _ := newTestService(ev, enabledConfig())

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "rm -rf /data"})

	assert.Equal(t, OutcomeReject, r.Outcome)
	assert.Equal(t, []string{QuestionDestructive}, r.Failed)
}

func TestReviewAsksBeyondRequestOnlyWhenUserRequestIsKnown(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{QuestionBeyondRequest: 0.9}}
	s, _ := newTestService(ev, enabledConfig())

	r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.NotContains(t, ev.last.Questions, QuestionBeyondRequest)
	assert.Equal(t, OutcomePass, r.Outcome)

	r = s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls", UserRequest: "看一下磁盘空间"})
	assert.Contains(t, ev.last.Questions, QuestionBeyondRequest)
	assert.Equal(t, OutcomeReject, r.Outcome)
	assert.Equal(t, []string{QuestionBeyondRequest}, r.Failed)
}

func TestReviewSendsRedactedCommandWithAssetType(t *testing.T) {
	ev := &fakeEvaluator{}
	s, _ := newTestService(ev, enabledConfig())

	s.Review(context.Background(), Input{AssetType: "database", Command: "mysql -uroot -pS3cret -e 'select 1'"})

	state, ok := ev.last.State.(reviewState)
	require.True(t, ok)
	assert.Equal(t, "database", state.AssetType)
	assert.Equal(t, "mysql -uroot -p*** -e 'select 1'", state.Command)
	assert.Equal(t, "jev-1.13.0", ev.last.Model)
}

func TestReviewFailsWithoutCallingModel(t *testing.T) {
	cases := []struct {
		name   string
		cfg    Config
		cmd    string
		reason string
	}{
		{"not configured", Config{}, "ls", ReasonNotConfigured},
		{"too long", enabledConfig(), strings.Repeat("a", 4001), ReasonTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := &fakeEvaluator{}
			s, _ := newTestService(ev, c.cfg)
			r := s.Review(context.Background(), Input{AssetType: "ssh", Command: c.cmd})
			assert.Equal(t, OutcomeFail, r.Outcome)
			assert.Equal(t, c.reason, r.Reason)
			assert.Zero(t, ev.calls)
		})
	}
}

func TestReviewFailsOnModelErrors(t *testing.T) {
	cases := []struct {
		name   string
		ev     *fakeEvaluator
		reason string
	}{
		{"timeout", &fakeEvaluator{delay: time.Second}, ReasonTimeout},
		{"auth", &fakeEvaluator{err: &typesafe.APIError{StatusCode: http.StatusUnauthorized}}, ReasonInvalidAPIKey},
		{"other", &fakeEvaluator{err: errors.New("connection refused")}, ReasonUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := enabledConfig()
			cfg.Timeout = 20 * time.Millisecond
			s, _ := newTestService(c.ev, cfg)
			r := s.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
			assert.Equal(t, OutcomeFail, r.Outcome)
			assert.Equal(t, c.reason, r.Reason)
		})
	}
}

func TestReviewUsesCacheForSameCommandAndSkipsCachingFailures(t *testing.T) {
	ev := &fakeEvaluator{nouls: map[string]float64{QuestionDisruptive: 0.9}}
	s, cache := newTestService(ev, enabledConfig())

	first := s.Review(context.Background(), Input{AssetType: "ssh", Command: "systemctl stop nginx"})
	second := s.Review(context.Background(), Input{AssetType: "ssh", Command: "systemctl stop nginx"})
	assert.Equal(t, 1, ev.calls)
	assert.Equal(t, first.Outcome, second.Outcome)
	assert.True(t, second.Cached)
	assert.Len(t, cache.m, 1)

	failing := &fakeEvaluator{err: errors.New("boom")}
	s2, cache2 := newTestService(failing, enabledConfig())
	s2.Review(context.Background(), Input{AssetType: "ssh", Command: "ls"})
	assert.Empty(t, cache2.m)
}

func TestCacheKeyDependsOnModelAssetTypeCommandAndRequest(t *testing.T) {
	base := cacheKey("jev-1.13.0", "ssh", "ls", "")
	assert.NotEqual(t, base, cacheKey("jev-1.14.0", "ssh", "ls", ""))
	assert.NotEqual(t, base, cacheKey("jev-1.13.0", "redis", "ls", ""))
	assert.NotEqual(t, base, cacheKey("jev-1.13.0", "ssh", "ls -la", ""))
	assert.NotEqual(t, base, cacheKey("jev-1.13.0", "ssh", "ls", "看磁盘"))
	assert.Equal(t, base, cacheKey("jev-1.13.0", "ssh", "ls", ""))
}

func TestNewConfigFillsDefaults(t *testing.T) {
	cfg := NewConfig("k", "", 0, 0)
	assert.Equal(t, Config{APIKey: "k", Model: DefaultModel, Threshold: DefaultThreshold, Timeout: DefaultTimeout, MaxCommandLen: MaxCommandLen}, cfg)

	cfg = NewConfig("k", "jev-1.14.0", 3000, 0.5)
	assert.Equal(t, "jev-1.14.0", cfg.Model)
	assert.Equal(t, 3*time.Second, cfg.Timeout)
	assert.InDelta(t, 0.5, cfg.Threshold, 1e-9)
}

func TestTestConnection(t *testing.T) {
	s, _ := newTestService(&fakeEvaluator{}, Config{})
	assert.ErrorIs(t, s.TestConnection(context.Background()), ErrNotConfigured)

	ok := &fakeEvaluator{}
	s, _ = newTestService(ok, enabledConfig())
	assert.NoError(t, s.TestConnection(context.Background()))
	assert.Equal(t, 1, ok.calls)

	authErr := &typesafe.APIError{StatusCode: http.StatusUnauthorized}
	s, _ = newTestService(&fakeEvaluator{err: authErr}, enabledConfig())
	assert.ErrorIs(t, s.TestConnection(context.Background()), authErr)
}
