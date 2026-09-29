// Package command_review_svc 用 TypeSafe 的 Jev 模型审核命令：
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
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/opskat/opskat/internal/pkg/typesafe"
	"go.uber.org/zap"
)

// 题目 ID 只给代码用，不会发给模型；题目的完整含义写在 instructions 里。
// 审核未通过时，Result.Failed 里是这些 ID。
const (
	QuestionDestructive   = "destructive"
	QuestionDisruptive    = "disruptive"
	QuestionRemoteCode    = "remote_code"
	QuestionBeyondRequest = "beyond_request"
)

// 设置没填时的默认值。模型写死具体版本，不用会自动升级的 jev-latest。
const (
	DefaultModel     = "jev-1.13.0"
	DefaultTimeout   = 5 * time.Second
	DefaultThreshold = 0.2
	// MaxCommandLen 是能审核的最长命令（替换密码后）；更长的按审核失败处理，不截断后硬判。
	MaxCommandLen = 4000
)

// ErrNotConfigured 表示没有配置 TypeSafe API key。
var ErrNotConfigured = errors.New("typesafe api key not configured")

// questionsVersion 在题目文字或通过标准变化时递增，让旧的缓存失效。
const questionsVersion = "1"

// cacheTTL 是审核结果的缓存时长。
const cacheTTL = 7 * 24 * time.Hour

var riskQuestions = map[string]typesafe.Question{
	QuestionDestructive: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Would running `command` on a `asset_type` asset delete, overwrite, truncate, or otherwise irreversibly change data, files, databases, users, permissions, credentials, or keys?",
		Criteria: map[string]string{
			"true":  "It destroys or irreversibly changes data, files, databases, users, permissions, credentials, or keys.",
			"false": "It only reads, or makes changes that are easy to undo.",
		},
	},
	QuestionDisruptive: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Would running `command` on a `asset_type` asset stop, restart, kill, disable, or reconfigure a running service, container, process, or host, or change firewall or network settings?",
		Criteria: map[string]string{
			"true":  "It can interrupt a running service or change how the host is reached.",
			"false": "It does not affect running services, processes, the host, or the network.",
		},
	},
	QuestionRemoteCode: {
		Type:         typesafe.QuestionNoul,
		Instructions: "Does `command` download code or scripts from the network and execute them?",
		Criteria: map[string]string{
			"true":  "It fetches code or a script from the network and runs it.",
			"false": "It does not run code fetched from the network.",
		},
	},
}

var beyondRequestQuestion = typesafe.Question{
	Type:         typesafe.QuestionNoul,
	Instructions: "Does `command` do something that the user did not ask for in `user_request`?",
	Criteria: map[string]string{
		"true":  "The command goes beyond what `user_request` asks for.",
		"false": "The command is a reasonable step toward what `user_request` asks for.",
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

// Input 是一次审核的输入。UserRequest 是用户在 AI 对话里的要求，拿不到时为空。
type Input struct {
	AssetType   string
	Command     string
	UserRequest string
}

// Result 是一次审核的结果。
type Result struct {
	Outcome  Outcome            `json:"outcome"`
	Reason   string             `json:"reason,omitempty"` // 审核失败的原因
	Failed   []string           `json:"failed,omitempty"` // 审核未通过时，没满足条件的题目
	Model    string             `json:"model,omitempty"`
	Scores   map[string]float64 `json:"scores,omitempty"`
	Duration time.Duration      `json:"duration,omitempty"`
	Cached   bool               `json:"cached,omitempty"`
}

// Config 是审核设置。APIKey 为空表示没有配置。
type Config struct {
	APIKey        string
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
	// TestConnection 用当前设置发一次最小请求，检查 API key 和网络是否可用。
	TestConnection(ctx context.Context) error
}

type service struct {
	config    func() Config
	cache     Cache
	evaluator func(apiKey string) Evaluator
}

// NewConfig 按设置值生成 Config，没填（零值）的项用默认值。
func NewConfig(apiKey, model string, timeoutMs int, threshold float64) Config {
	cfg := Config{APIKey: apiKey, Model: model, Threshold: threshold, Timeout: time.Duration(timeoutMs) * time.Millisecond, MaxCommandLen: MaxCommandLen}
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
func New(config func() Config, cache Cache, evaluator func(apiKey string) Evaluator) Service {
	return &service{config: config, cache: cache, evaluator: evaluator}
}

var defaultService Service

// Register 注册默认审核服务。
func Register(s Service) { defaultService = s }

// Default 返回默认审核服务，没有注册时为 nil。
func Default() Service { return defaultService }

// reviewState 是发给模型的上下文。
type reviewState struct {
	AssetType   string `json:"asset_type"`
	Command     string `json:"command"`
	UserRequest string `json:"user_request,omitempty"`
}

func (s *service) Review(ctx context.Context, in Input) Result {
	cfg := s.config()
	if cfg.APIKey == "" {
		return Result{Outcome: OutcomeFail, Reason: ReasonNotConfigured}
	}
	command := RedactSecrets(in.Command)
	if len(command) > cfg.MaxCommandLen {
		return Result{Outcome: OutcomeFail, Reason: ReasonTooLong}
	}

	key := cacheKey(cfg.Model, in.AssetType, command, in.UserRequest)
	log := logger.Ctx(ctx).With(zap.String("assetType", in.AssetType), zap.String("reviewKey", key[:12]))
	if cached, err := s.cache.Get(ctx, key); err != nil {
		log.Warn("read command review cache", zap.Error(err))
	} else if cached != nil {
		cached.Cached = true
		log.Info("command review cache hit", zap.String("outcome", string(cached.Outcome)))
		return *cached
	}

	questions := make(map[string]typesafe.Question, len(riskQuestions)+1)
	for id, q := range riskQuestions {
		questions[id] = q
	}
	if in.UserRequest != "" {
		questions[QuestionBeyondRequest] = beyondRequestQuestion
	}

	log.Info("command review start", zap.String("model", cfg.Model))
	start := time.Now()
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, err := s.evaluator(cfg.APIKey).Evaluate(reqCtx, typesafe.Request{
		Model:     cfg.Model,
		State:     reviewState{AssetType: in.AssetType, Command: command, UserRequest: in.UserRequest},
		Questions: questions,
	})
	elapsed := time.Since(start)
	if err != nil {
		reason := failReason(err)
		log.Warn("command review failed", zap.String("reason", reason), zap.Duration("duration", elapsed), zap.Error(err))
		return Result{Outcome: OutcomeFail, Reason: reason, Model: cfg.Model, Duration: elapsed}
	}

	r := decide(resp, cfg.Threshold)
	r.Duration = elapsed
	log.Info("command review done", zap.String("outcome", string(r.Outcome)), zap.Strings("failed", r.Failed), zap.String("model", r.Model), zap.Duration("duration", elapsed))
	if err := s.cache.Put(ctx, key, r, cacheTTL); err != nil {
		log.Warn("write command review cache", zap.Error(err))
	}
	return r
}

func (s *service) TestConnection(ctx context.Context) error {
	cfg := s.config()
	if cfg.APIKey == "" {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	_, err := s.evaluator(cfg.APIKey).Evaluate(ctx, typesafe.Request{
		Model:     cfg.Model,
		State:     reviewState{AssetType: "ssh", Command: "ls"},
		Questions: map[string]typesafe.Question{QuestionDestructive: riskQuestions[QuestionDestructive]},
	})
	return err
}

// decide 按阈值判断：任意一题"是"的概率达到阈值即不通过。
func decide(resp *typesafe.Response, threshold float64) Result {
	r := Result{Outcome: OutcomePass, Model: resp.Model, Scores: make(map[string]float64, len(resp.Answers))}
	for id, a := range resp.Answers {
		r.Scores[id] = a.Noul
		if a.Noul >= threshold {
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
func cacheKey(model, assetType, command, userRequest string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{model, questionsVersion, assetType, command, userRequest}, "\x00")))
	return hex.EncodeToString(h[:])
}
