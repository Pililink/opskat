// Package command_review_svc 用 Jev 格式（TypeSafe System One API）的模型审核命令：
// 命令没被规则放行、原本要问人时，由它判断能不能自动执行。
// 不区分资产类型：所有类型用同一组题目、同一个通过标准。
package command_review_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/opskat/opskat/internal/pkg/typesafe"
	"go.uber.org/zap"
)

// 题目 ID 只给代码用，不会发给模型；题目的完整含义写在 instructions 里。
// 审核未通过时，Result.Failed 里是这些 ID。
const (
	QuestionDestructive = "destructive"
	QuestionDisruptive  = "disruptive"
	QuestionRemoteCode  = "remote_code"
)

// 设置没填时的默认值：TypeSafe 官方地址上的 Jev 具体版本。
const (
	DefaultBaseURL   = typesafe.DefaultBaseURL
	DefaultModel     = "jev-1.13.0"
	DefaultTimeout   = 5 * time.Second
	DefaultThreshold = 0.2
	// MaxCommandLen 是能审核的最长命令（替换密码后）；更长的按审核失败处理，不截断后硬判。
	MaxCommandLen = 4000
)

// ErrNotConfigured 表示没有配置审核模型的 API key。
var ErrNotConfigured = errors.New("typesafe api key not configured")

// questionsVersion 在题目文字或通过标准变化时递增，让旧的缓存失效。
const questionsVersion = "2"

// cacheTTL 是审核结果的缓存时长。
const cacheTTL = 7 * 24 * time.Hour

// maxParallelReviews 是批量审核时同时调用模型的上限。
const maxParallelReviews = 4

// riskQuestions 只问明确的危险：破坏数据、中断服务、下载并执行代码。只读的查看类命令
// （状态、日志、配置、进程、容器、定时任务、网络连接）在每道题里都明确写成"否"，
// 这类命令不应被拦下来。
var riskQuestions = map[string]typesafe.Question{
	QuestionDestructive: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Would running `command` on a `asset_type` asset clearly destroy data or make an irreversible change, such as deleting files or directories, wiping or formatting disks, dropping or truncating databases, tables, or collections, bulk-deleting keys, records, or storage objects, or deleting users, credentials, or keys?",
		Criteria: map[string]string{
			"true":  "The command clearly deletes, wipes, overwrites, or irreversibly changes data, files, databases, users, credentials, or keys.",
			"false": "The command only reads, lists, searches, or shows status, logs, configuration, processes, containers, scheduled jobs, or network state, or it makes a small change that is easy to undo.",
		},
	},
	QuestionDisruptive: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Would running `command` on a `asset_type` asset clearly interrupt a running system, such as stopping, restarting, killing, or disabling a service, container, process, or the host, or changing firewall, routing, or network settings so that the host or its services become unreachable?",
		Criteria: map[string]string{
			"true":  "The command stops, restarts, kills, or disables something that is running, or cuts off network access to the host or its services.",
			"false": "The command only inspects state, such as service status, timers, containers, connections, or VPN status, and leaves services, processes, the host, and the network running as they are.",
		},
	},
	QuestionRemoteCode: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Does `command` download code or a script from the network and run it, for example by piping `curl` or `wget` output into a shell?",
		Criteria: map[string]string{
			"true":  "The command fetches code or a script from the network and runs it.",
			"false": "The command does not run code fetched from the network; downloading a file without running it, or querying a remote service, does not count.",
		},
	},
}

// Outcome 是审核结果。
type Outcome string

const (
	OutcomePass   Outcome = "pass"   // 审核通过
	OutcomeReject Outcome = "reject" // 审核未通过
	OutcomeFail   Outcome = "fail"   // 审核失败：没能得到判断
)

// 审核失败的原因。
const (
	ReasonNotConfigured = "not_configured"
	ReasonTooLong       = "too_long"
	ReasonTimeout       = "timeout"
	ReasonInvalidAPIKey = "invalid_api_key"
	ReasonUnavailable   = "unavailable"
)

// Input 是一次审核的输入。
type Input struct {
	AssetType string
	Command   string
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
}

// Config 是审核设置。APIKey 为空表示没有配置。
type Config struct {
	APIKey        string
	BaseURL       string // 兼容 TypeSafe System One API 的服务地址
	Model         string
	Threshold     float64 // 任意一题"是"的概率达到它就不通过
	Timeout       time.Duration
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
	// Status 返回本进程内最近一次审核失败的情况；之后审核成功过则为空。
	Status() Status
	// SetConfigErrorListener 注册配置错误（未配置 / API key 无效）的通知，同一种错误只通知一次，
	// 审核恢复成功后再出错会重新通知。
	SetConfigErrorListener(fn func(reason string))
}

// Status 是最近一次审核失败的情况，设置页据此提示配置问题。
type Status struct {
	LastFailReason string    `json:"lastFailReason,omitempty"`
	LastFailAt     time.Time `json:"lastFailAt,omitempty"`
}

type service struct {
	config    func() Config
	cache     Cache
	evaluator func(apiKey, baseURL string) Evaluator

	mu       sync.Mutex
	status   Status
	listener func(reason string)
	notified map[string]bool
}

// NewConfig 按设置值生成 Config，没填（零值）的项用默认值。
func NewConfig(apiKey, baseURL, model string, timeoutMs int, threshold float64) Config {
	cfg := Config{APIKey: apiKey, BaseURL: baseURL, Model: model, Threshold: threshold, Timeout: time.Duration(timeoutMs) * time.Millisecond, MaxCommandLen: MaxCommandLen}
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

// New 创建审核服务。config 每次审核时读取，设置修改后立即生效。
func New(config func() Config, cache Cache, evaluator func(apiKey, baseURL string) Evaluator) Service {
	return &service{config: config, cache: cache, evaluator: evaluator, notified: map[string]bool{}}
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
	if (r.Reason == ReasonNotConfigured || r.Reason == ReasonInvalidAPIKey) && !s.notified[r.Reason] {
		s.notified[r.Reason] = true
		notify = s.listener
	}
	s.mu.Unlock()
	if notify != nil {
		notify(r.Reason)
	}
	return r
}

// recordSuccess 清掉失败状态，之后再出配置错误会重新通知。
func (s *service) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = Status{}
	s.notified = map[string]bool{}
}

var defaultService Service

// Register 注册默认审核服务。
func Register(s Service) { defaultService = s }

// Default 返回默认审核服务，没有注册时为 nil。
func Default() Service { return defaultService }

// reviewState 是发给模型的上下文。
type reviewState struct {
	AssetType string `json:"asset_type"`
	Command   string `json:"command"`
}

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
	cfg := s.config()
	results := make([]Result, len(ins))
	var pending []pendingReview
	for i, in := range ins {
		if cfg.APIKey == "" {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonNotConfigured})
			continue
		}
		command := RedactSecrets(in.Command)
		if len(command) > cfg.MaxCommandLen {
			results[i] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: ReasonTooLong})
			continue
		}
		key := cacheKey(cfg.BaseURL, cfg.Model, in.AssetType, command)
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
		resp    *typesafe.Response
		err     error
		elapsed time.Duration
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
			resp, err := s.evaluate(ctx, cfg, p)
			evals[j] = evaluation{resp: resp, err: err, elapsed: time.Since(start)}
		}(j, p)
	}
	wg.Wait()

	for j, p := range pending {
		e := evals[j]
		if e.err != nil {
			reason := failReason(e.err)
			p.log.Warn("command review failed", zap.String("reason", reason), zap.Duration("duration", e.elapsed), zap.Error(e.err))
			results[p.idx] = s.recordFailure(Result{Outcome: OutcomeFail, Reason: reason, Model: cfg.Model, Duration: e.elapsed})
			continue
		}
		s.recordSuccess()
		scores := make(map[string]float64, len(e.resp.Answers))
		for id, a := range e.resp.Answers {
			scores[id] = a.Noul
		}
		r := decide(e.resp.Model, scores, cfg.Threshold)
		r.Duration = e.elapsed
		p.log.Info("command review done", zap.String("outcome", string(r.Outcome)), zap.Strings("failed", r.Failed), zap.String("model", r.Model), zap.Duration("duration", e.elapsed))
		if err := s.cache.Put(ctx, p.key, r, cacheTTL); err != nil {
			p.log.Warn("write command review cache", zap.Error(err))
		}
		results[p.idx] = r
	}
	return results
}

// evaluate 为一条命令调用模型，超时由设置控制。
func (s *service) evaluate(ctx context.Context, cfg Config, p pendingReview) (*typesafe.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	return s.evaluator(cfg.APIKey, cfg.BaseURL).Evaluate(reqCtx, typesafe.Request{
		Model:     cfg.Model,
		State:     reviewState{AssetType: p.in.AssetType, Command: p.command},
		Questions: riskQuestions,
	})
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
		Questions: map[string]typesafe.Question{QuestionDestructive: riskQuestions[QuestionDestructive]},
	})
	if err != nil {
		return "", err
	}
	return resp.Model, nil
}

// decide 按阈值判断：任意一题"是"的概率达到阈值即不通过。
func decide(model string, scores map[string]float64, threshold float64) Result {
	r := Result{Outcome: OutcomePass, Model: model, Scores: scores, Threshold: threshold}
	for id, p := range scores {
		if p >= threshold {
			r.Failed = append(r.Failed, id)
		}
	}
	if len(r.Failed) > 0 {
		sort.Strings(r.Failed)
		r.Outcome = OutcomeReject
	}
	return r
}

func failReason(err error) string {
	var apiErr *typesafe.APIError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case errors.As(err, &apiErr) && apiErr.IsAuth():
		return ReasonInvalidAPIKey
	default:
		return ReasonUnavailable
	}
}

// cacheKey 由模型版本、题目版本、资产类型、替换密码后的命令和用户要求算出。
func cacheKey(baseURL, model, assetType, command string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{baseURL, model, questionsVersion, assetType, command}, "\x00")))
	return hex.EncodeToString(h[:])
}
