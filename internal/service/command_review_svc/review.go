// Package command_review_svc 用 Jev 格式（TypeSafe System One API）的模型审核命令：
// 命令没被规则放行、原本要问人时，由它判断能不能自动执行。
// 不区分资产类型：所有类型用同一组题目、同一个通过标准。
package command_review_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/opskat/opskat/internal/ai/aictx"
	"github.com/opskat/opskat/internal/pkg/jevreview"
	"github.com/opskat/opskat/internal/pkg/typesafe"
	"github.com/opskat/opskat/pkg/extension"
	"go.uber.org/zap"
)

// 题目 ID 只给代码用，不会发给模型；题目的完整含义写在 instructions 里。
// 审核未通过时，Result.Failed 里是这些 ID。
const (
	QuestionDestructive = jevreview.QuestionDestructive
	QuestionDisruptive  = jevreview.QuestionDisruptive
	QuestionRemoteCode  = jevreview.QuestionRemoteCode
)

// 设置没填时的默认值：TypeSafe 官方地址上的 Jev 具体版本。
const (
	DefaultBaseURL   = jevreview.DefaultBaseURL
	DefaultModel     = jevreview.DefaultModel
	DefaultTimeout   = 5 * time.Second
	DefaultThreshold = jevreview.DefaultThreshold
	// MaxCommandLen 是能审核的最长命令（替换敏感信息后）；更长的按审核失败处理，不截断后硬判。
	MaxCommandLen = 4000
)

// ErrNotConfigured 表示没有配置审核模型的 API key。
var ErrNotConfigured = errors.New("typesafe api key not configured")

// cacheTTL 是审核结果的缓存时长。
const cacheTTL = 7 * 24 * time.Hour

// maxParallelReviews 是批量审核时同时调用模型的上限。
const maxParallelReviews = 4

// maxAttempts 是一次审核最多调用模型的次数：超时或连接出错时再试一次。opsctl 每次都是新进程，
// 第一次请求要重新解析域名、建立连接，偶尔会超时；第二次通常很快。
const maxAttempts = 2

// Outcome 是审核结果，和审批、审计里记的是同一个类型。
type Outcome = aictx.ReviewOutcome

const (
	OutcomePass   = aictx.ReviewPass   // 审核通过
	OutcomeReject = aictx.ReviewReject // 审核未通过
	OutcomeFail   = aictx.ReviewFail   // 审核失败：没能得到判断
)

// 审核失败的原因。
const (
	ReasonNotConfigured = "not_configured"
	// 保存过 API key，但读不出来（如换了主密钥解不开）
	ReasonAPIKeyUnreadable = "api_key_unreadable" // #nosec G101 -- 失败原因的名字，不是凭据。
	ReasonTooLong          = "too_long"
	ReasonUnparseable      = "unparseable" // 命令解析不了，没法替换敏感信息，不发送
	ReasonUndecodable      = "undecodable" // 认得出解码后执行，但解不开要执行的内容，不发送
	ReasonTimeout          = jevreview.ReasonTimeout
	ReasonInvalidAPIKey    = jevreview.ReasonInvalidAPIKey
	ReasonUnavailable      = jevreview.ReasonUnavailable
	// ReasonInvalidResult 是扩展返回了不合规的整批结果。
	ReasonInvalidResult = "invalid_result"
	// ReasonReviewerUnavailable 是已选中审核扩展，但它没加载、不是审核器，或来源/摘要检查没过。
	// 这时审核失败，不改用内置审核。
	ReasonReviewerUnavailable = "reviewer_unavailable"
)

// ErrReviewerUnavailable 是选中的审核扩展这次不可用。Review 把它记成 ReasonReviewerUnavailable。
var ErrReviewerUnavailable = errors.New("selected command reviewer is unavailable")

// isConfigError 判断审核失败是不是设置有问题：只有用户改设置才能恢复，要提醒用户。
func isConfigError(reason string) bool {
	return reason == ReasonNotConfigured || reason == ReasonAPIKeyUnreadable || reason == ReasonInvalidAPIKey ||
		reason == ReasonReviewerUnavailable
}

// Input 是一次审核的输入。Syntax 决定怎样找出命令里的敏感信息（见 RedactSensitive）。
type Input struct {
	AssetType string
	Command   string
	Syntax    Syntax
}

// Result 是一次审核的结果。
type Result struct {
	Outcome   Outcome            `json:"outcome"`
	Reason    string             `json:"reason,omitempty"` // 审核失败的原因
	Failed    []string           `json:"failed,omitempty"` // 审核未通过时，没满足条件的题目
	Model     string             `json:"model,omitempty"`
	Scores    map[string]float64 `json:"scores,omitempty"`    // 每道题回答"是"的概率
	Threshold float64            `json:"threshold,omitempty"` // 判断用的阈值
	Duration  time.Duration      `json:"duration,omitempty"`
	Cached    bool               `json:"cached,omitempty"`
	Attempts  int                `json:"attempts,omitempty"` // 调用模型的次数，超时或连接出错时会重试
	// Extension / Version 是做出这次判断的审核扩展。内置 TypeSafe 审核为空。
	Extension string `json:"extension,omitempty"`
	Version   string `json:"version,omitempty"`
	// FailedNames 是审核扩展为 Failed 里每道题声明的各语言名称（题目 id → 语言 → 名称）。内置审核为空。
	FailedNames map[string]map[string]string `json:"failed_names,omitempty"`
}

// ExtensionReviewer 是当前生效的扩展审核器。没有选中时返回 nil，审核走内置 TypeSafe。
type ExtensionReviewer interface {
	// Identity 是扩展名、版本和设置哈希。设置哈希参与缓存键。
	Identity() (name, version, settingsHash string)
	// Limits 是 describe() 声明的单批条数和单条命令最大长度。
	Limits() (maxItems, maxCommandLen int)
	// Questions 是 describe() 声明的题目，结果里只回题目 id，宿主按这里的名称显示。
	Questions() []extension.ReviewQuestion
	Review(ctx context.Context, items []extension.ReviewItem) ([]extension.ReviewCommandResult, error)
}

// Info 是这次审核在审批和审计里记的样子；mode 是触发审核的权限模式。
func (r Result) Info(mode string) *aictx.ReviewInfo {
	return &aictx.ReviewInfo{
		Mode:        mode,
		Outcome:     r.Outcome,
		Reason:      r.Reason,
		Failed:      r.Failed,
		Model:       r.Model,
		Scores:      r.Scores,
		Threshold:   r.Threshold,
		DurationMs:  r.Duration.Milliseconds(),
		Cached:      r.Cached,
		Attempts:    r.Attempts,
		Extension:   r.Extension,
		Version:     r.Version,
		FailedNames: r.FailedNames,
	}
}

// Config 是审核设置。APIKey 为空表示没有配置。
type Config struct {
	APIKey        string
	BaseURL       string // 兼容 TypeSafe System One API 的服务地址
	Model         string
	Threshold     float64       // 任意一题"是"的概率达到它就不通过
	Timeout       time.Duration // 每次调用模型的超时；超时后会重试（见 maxAttempts）
	MaxCommandLen int
}

// Evaluator 是模型调用，*typesafe.Client 实现了它。
type Evaluator interface {
	Evaluate(ctx context.Context, req typesafe.Request) (*typesafe.Response, error)
}

// Cache 保存审核结果。Get 找不到时返回 (nil, nil)。
type Cache interface {
	Get(ctx context.Context, key string) (*Result, error)
	Put(ctx context.Context, key string, r Result, ttl time.Duration) error
}

// Service 审核命令。
type Service interface {
	Review(ctx context.Context, in Input) Result
	// ReviewBatch 审核多条命令，结果与输入一一对应；调用模型这一步并行。
	ReviewBatch(ctx context.Context, ins []Input) []Result
	// TestModel 用给定的设置（可以是设置页上还没保存的值）发一次最小请求，
	// 检查地址、API key 和模型是否可用，返回服务端实际作答的模型版本。
	TestModel(ctx context.Context, cfg Config) (string, error)
	// Status 返回本进程启动以来最近一次审核失败的情况，没失败过则为空。
	Status() Status
	// SetConfigErrorListener 注册配置错误（未配置 / API key 读不出来 / API key 无效）的通知，
	// 同一种错误只通知一次，审核恢复成功后再出错会重新通知。
	SetConfigErrorListener(fn func(reason string))
}

// Status 是最近一次审核失败的情况，设置页据此提示配置问题。
type Status struct {
	LastFailReason string    `json:"lastFailReason,omitempty"`
	LastFailAt     time.Time `json:"lastFailAt,omitempty"`
}

type service struct {
	config    func() (Config, error)
	cache     Cache
	evaluator func(apiKey, baseURL string) Evaluator
	// extension 每次审核时读取当前生效的扩展审核器。
	// 返回 (nil, nil) 表示没有选中扩展，走内置 TypeSafe。
	// 返回 ErrReviewerUnavailable 表示选中了但这次不可用，整批审核失败，不退回内置。
	extension func(ctx context.Context) (ExtensionReviewer, error)

	mu       sync.Mutex
	status   Status
	listener func(reason string)
	notified map[string]bool
}

// Settings 是用户填的审核设置，没填的项为零值。
type Settings struct {
	APIKey    string
	BaseURL   string
	Model     string
	TimeoutMs int
	Threshold float64
}

// NewConfig 按设置生成 Config，没填（零值）的项用默认值。
func NewConfig(s Settings) Config {
	cfg := Config{APIKey: s.APIKey, BaseURL: s.BaseURL, Model: s.Model, Threshold: s.Threshold, Timeout: time.Duration(s.TimeoutMs) * time.Millisecond, MaxCommandLen: MaxCommandLen}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = DefaultThreshold
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	return cfg
}

// ExtensionReviewerFunc 读取当前生效的扩展审核器。
// (nil, nil) 表示没有选中；ErrReviewerUnavailable 表示选中了但不可用。
type ExtensionReviewerFunc func(ctx context.Context) (ExtensionReviewer, error)

// New 创建审核服务。config 每次审核时读取，设置修改后立即生效；它返回错误表示保存过的
// API key 读不出来，这时内置审核失败（ReasonAPIKeyUnreadable），不会当成没配置。
// extension 可为 nil，表示这个进程不接扩展审核器。
func New(config func() (Config, error), cache Cache, evaluator func(apiKey, baseURL string) Evaluator, extension ExtensionReviewerFunc) Service {
	return &service{config: config, cache: cache, evaluator: evaluator, extension: extension, notified: map[string]bool{}}
}

func (s *service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *service) SetConfigErrorListener(fn func(reason string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener = fn
}

// recordFailure 记下审核失败；配置错误通知一次。
func (s *service) recordFailure(r Result) Result {
	s.mu.Lock()
	s.status = Status{LastFailReason: r.Reason, LastFailAt: time.Now()}
	var notify func(string)
	if isConfigError(r.Reason) && !s.notified[r.Reason] {
		s.notified[r.Reason] = true
		notify = s.listener
	}
	s.mu.Unlock()
	if notify != nil {
		notify(r.Reason)
	}
	return r
}

// recordSuccess 让之后再出的配置错误重新通知。最近一次失败留着，设置页照样显示。
func (s *service) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notified = map[string]bool{}
}

var defaultService Service

// Register 注册默认审核服务。
func Register(s Service) { defaultService = s }

// Default 返回默认审核服务，没有注册时为 nil。
func Default() Service { return defaultService }

// reviewState 是发给模型的上下文。
type reviewState = jevreview.State

func (s *service) Review(ctx context.Context, in Input) Result {
	return s.ReviewBatch(ctx, []Input{in})[0]
}

// pendingReview 是一条需要调用模型的审核。
type pendingReview struct {
	idx     int
	key     string
	command string
	in      Input
	log     *zap.Logger
}

// ReviewBatch 审核多条命令，结果与输入一一对应。读写缓存按顺序做（SQLite 不适合并发写），
// 只有调用模型这一步并行，最多 maxParallelReviews 条同时进行。
func (s *service) ReviewBatch(ctx context.Context, ins []Input) []Result {
	if s.extension != nil {
		rev, err := s.extension(ctx)
		if err != nil {
			reason := ReasonUnavailable
			if errors.Is(err, ErrReviewerUnavailable) {
				reason = ReasonReviewerUnavailable
			}
			logger.Ctx(ctx).Warn("selected command reviewer unavailable", zap.String("reason", reason), zap.Error(err))
			results := make([]Result, len(ins))
			for i := range ins {
				results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason})
			}
			return results
		}
		if rev != nil {
			return s.reviewWithExtension(ctx, rev, ins)
		}
	}
	return s.reviewWithBuiltin(ctx, ins)
}

func (s *service) reviewWithBuiltin(ctx context.Context, ins []Input) []Result {
	results := make([]Result, len(ins))
	cfg, err := s.config()
	if err != nil {
		logger.Ctx(ctx).Error("read command review api key", zap.Error(err))
		for i := range results {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonAPIKeyUnreadable})
		}
		return results
	}
	var pending []pendingReview
	for i, in := range ins {
		if cfg.APIKey == "" {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonNotConfigured})
			continue
		}
		reviewed, err := commandForReview(in)
		if err != nil {
			reason := ReasonUnparseable
			if errors.Is(err, ErrUndecodable) {
				reason = ReasonUndecodable
			}
			logger.Ctx(ctx).Warn("prepare command for review", zap.String("assetType", in.AssetType), zap.String("reason", reason), zap.Error(err))
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason})
			continue
		}
		command, err := RedactSensitive(in.Syntax, reviewed)
		if err != nil {
			logger.Ctx(ctx).Warn("redact command for review", zap.String("assetType", in.AssetType), zap.Error(err))
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonUnparseable})
			continue
		}
		if len(command) > cfg.MaxCommandLen {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonTooLong})
			continue
		}
		key := cacheKey(cfg.BaseURL, cfg.Model, in.AssetType, cacheKeyCommand(in.Command, reviewed))
		log := logger.Ctx(ctx).With(zap.String("assetType", in.AssetType), zap.String("reviewKey", key[:12]))
		if cached, err := s.cache.Get(ctx, key); err != nil {
			log.Warn("read command review cache", zap.Error(err))
		} else if cached != nil {
			// 缓存的是评分：按当前阈值重新判断，设置里改了阈值马上生效。
			r := decide(cached.Model, cached.Scores, cfg.Threshold)
			r.Cached = true
			log.Info("command review cache hit", zap.String("outcome", string(r.Outcome)))
			results[i] = r
			continue
		}
		pending = append(pending, pendingReview{idx: i, key: key, command: command, in: in, log: log})
	}

	type evaluation struct {
		resp     *typesafe.Response
		err      error
		elapsed  time.Duration
		attempts int
	}
	evals := make([]evaluation, len(pending))
	sem := make(chan struct{}, maxParallelReviews)
	var wg sync.WaitGroup
	for j, p := range pending {
		wg.Add(1)
		go func(j int, p pendingReview) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p.log.Info("command review start", zap.String("model", cfg.Model))
			start := time.Now()
			resp, attempts, err := s.evaluate(ctx, cfg, p)
			evals[j] = evaluation{resp: resp, err: err, elapsed: time.Since(start), attempts: attempts}
		}(j, p)
	}
	wg.Wait()

	for j, p := range pending {
		e := evals[j]
		if e.err != nil {
			reason := jevreview.FailReason(e.err)
			p.log.Warn("command review failed", zap.String("reason", reason), zap.Int("attempts", e.attempts), zap.Duration("duration", e.elapsed), zap.Error(e.err))
			results[p.idx] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason, Model: cfg.Model, Duration: e.elapsed, Attempts: e.attempts})
			continue
		}
		s.recordSuccess()
		scores := make(map[string]float64, len(e.resp.Answers))
		for id, a := range e.resp.Answers {
			scores[id] = a.Noul
		}
		r := decide(e.resp.Model, scores, cfg.Threshold)
		r.Duration = e.elapsed
		r.Attempts = e.attempts
		p.log.Info("command review done", zap.String("outcome", string(r.Outcome)), zap.Strings("failed", r.Failed), zap.String("model", r.Model), zap.Int("attempts", e.attempts), zap.Duration("duration", e.elapsed))
		if err := s.cache.Put(ctx, p.key, r, cacheTTL); err != nil {
			p.log.Warn("write command review cache", zap.Error(err))
		}
		results[p.idx] = r
	}
	return results
}

// evaluate 为一条命令调用模型，返回结果和调用次数。每次调用的超时由设置控制，
// 失败时按 retryable 决定是否再试，最多 maxAttempts 次。
func (s *service) evaluate(ctx context.Context, cfg Config, p pendingReview) (*typesafe.Response, int, error) {
	ev := s.evaluator(cfg.APIKey, cfg.BaseURL)
	req := jevreview.Request(cfg.Model, p.in.AssetType, p.command)
	for attempt := 1; ; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		resp, err := ev.Evaluate(reqCtx, req)
		cancel()
		if err == nil || attempt == maxAttempts || !jevreview.Retryable(ctx, err) {
			return resp, attempt, err
		}
		p.log.Warn("command review attempt failed, retrying", zap.Int("attempt", attempt), zap.Error(err))
	}
}

func (s *service) TestModel(ctx context.Context, cfg Config) (string, error) {
	if cfg.APIKey == "" {
		return "", ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, err := s.evaluator(cfg.APIKey, cfg.BaseURL).Evaluate(ctx, typesafe.Request{
		Model:     cfg.Model,
		State:     reviewState{AssetType: "ssh", Command: "ls"},
		Questions: map[string]typesafe.Question{QuestionDestructive: jevreview.Questions[QuestionDestructive]},
	})
	if err != nil {
		return "", err
	}
	return resp.Model, nil
}

// decide 按阈值判断：任意一题"是"的概率达到阈值即不通过。
func decide(model string, scores map[string]float64, threshold float64) Result {
	r := Result{Outcome: OutcomePass, Model: model, Scores: scores, Threshold: threshold, Failed: jevreview.Failed(scores, threshold)}
	if len(r.Failed) > 0 {
		r.Outcome = OutcomeReject
	}
	return r
}

// reviewWithExtension 用当前生效的扩展审核器审核。和内置审核一样先展开解码后执行的命令、
// 再替换敏感信息；超长命令不送审，按该条失败；其余按 maxItems 拆成多次 review_commands。
// 一次调用失败则该批全部失败。扩展审核用不到内置审核的 API key，config 只读超时，
// 所以 API key 解不开的错误在这里不影响审核。
func (s *service) reviewWithExtension(ctx context.Context, rev ExtensionReviewer, ins []Input) []Result {
	cfg, _ := s.config()
	name, version, settingsHash := rev.Identity()
	maxItems, maxLen := rev.Limits()
	questions := rev.Questions()
	results := make([]Result, len(ins))
	type pendingExt struct {
		idx  int
		item extension.ReviewItem
		key  string
	}
	var pending []pendingExt
	for i, in := range ins {
		reviewed, err := commandForReview(in)
		if err != nil {
			reason := ReasonUnparseable
			if errors.Is(err, ErrUndecodable) {
				reason = ReasonUndecodable
			}
			logger.Ctx(ctx).Warn("prepare command for review", zap.String("assetType", in.AssetType), zap.String("reason", reason), zap.Error(err))
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason, Extension: name, Version: version})
			continue
		}
		command, err := RedactSensitive(in.Syntax, reviewed)
		if err != nil {
			logger.Ctx(ctx).Warn("redact command for review", zap.String("assetType", in.AssetType), zap.Error(err))
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonUnparseable, Extension: name, Version: version})
			continue
		}
		if len(command) > maxLen {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonTooLong, Extension: name, Version: version})
			continue
		}
		key := extensionCacheKey(name, version, settingsHash, in.AssetType, cacheKeyCommand(in.Command, reviewed))
		if cached, err := s.cache.Get(ctx, key); err != nil {
			logger.Ctx(ctx).Warn("read command review cache", zap.String("extension", name), zap.Error(err))
		} else if cached != nil {
			cached.Cached = true
			cached.Extension = name
			cached.Version = version
			results[i] = *cached
			continue
		}
		pending = append(pending, pendingExt{idx: i, key: key, item: extension.ReviewItem{AssetType: in.AssetType, Command: command, Syntax: string(in.Syntax)}})
	}

	for start := 0; start < len(pending); start += maxItems {
		end := start + maxItems
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]
		items := make([]extension.ReviewItem, len(batch))
		for i, p := range batch {
			items[i] = p.item
		}
		reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		startAt := time.Now()
		out, err := rev.Review(reqCtx, items)
		elapsed := time.Since(startAt)
		cancel()
		if err != nil || len(out) != len(batch) {
			reason := ReasonInvalidResult
			if errors.Is(err, context.DeadlineExceeded) {
				reason = ReasonTimeout
			}
			logger.Ctx(ctx).Warn("extension command review failed", zap.String("extension", name), zap.String("reason", reason), zap.Error(err))
			for _, p := range batch {
				results[p.idx] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason, Extension: name, Version: version, Duration: elapsed, Attempts: 1})
			}
			continue
		}
		s.recordSuccess()
		for i, p := range batch {
			r := extensionResult(name, version, questions, out[i], elapsed)
			if r.Outcome == OutcomeFail {
				results[p.idx] = s.recordFailure(r)
				continue
			}
			if err := s.cache.Put(ctx, p.key, r, cacheTTL); err != nil {
				logger.Ctx(ctx).Warn("write command review cache", zap.String("extension", name), zap.Error(err))
			}
			results[p.idx] = r
		}
	}
	return results
}

// extensionResult 把审核扩展的回答落成宿主的结果：error 是"没能得到判断"，按审核失败处理。
func extensionResult(name, version string, questions []extension.ReviewQuestion, r extension.ReviewCommandResult, elapsed time.Duration) Result {
	out := Result{
		Outcome:   Outcome(r.Outcome),
		Reason:    r.Reason,
		Failed:    r.Failed,
		Extension: name,
		Version:   version,
		Duration:  elapsed,
		Attempts:  1,
	}
	if r.Outcome == extension.ReviewOutcomeError {
		out.Outcome = OutcomeFail
	}
	if len(r.Failed) > 0 {
		names := make(map[string]map[string]string, len(questions))
		for _, q := range questions {
			names[q.ID] = q.I18n.Name
		}
		out.FailedNames = make(map[string]map[string]string, len(r.Failed))
		for _, id := range r.Failed {
			out.FailedNames[id] = names[id]
		}
	}
	if r.Score != nil {
		out.Scores = map[string]float64{}
		for _, id := range r.Failed {
			out.Scores[id] = *r.Score
		}
	}
	return out
}

// extensionCacheKey 含扩展名、版本和设置哈希；命令部分同 cacheKey（原始命令，展开过的再带上展开后的文本）。
func extensionCacheKey(name, version, settingsHash, assetType, command string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{name, version, settingsHash, assetType, command}, "\x00")))
	return hex.EncodeToString(h[:])
}

// cacheKey 由服务地址、模型、题目版本、资产类型和命令算出。命令用原始命令而不是替换敏感信息后的：
// 替换后长得一样的两条命令（比如只有密码不同）各自审核，不共用结果。解码展开过的命令还带上
// 展开后的文本，避免展开前的评分被当成展开后的结果。表里只存哈希，不存命令。
func cacheKey(baseURL, model, assetType, command string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{baseURL, model, jevreview.QuestionsVersion, assetType, command}, "\x00")))
	return hex.EncodeToString(h[:])
}
