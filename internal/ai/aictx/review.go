package aictx

import "context"

// ReviewInfo 是一次模型审核的结果，随 CheckResult 传到审批界面和审计。
type ReviewInfo struct {
	Outcome    string             `json:"outcome"`          // pass / reject / fail
	Reason     string             `json:"reason,omitempty"` // 审核失败的原因
	Failed     []string           `json:"failed,omitempty"` // 审核未通过时没满足条件的题目
	Model      string             `json:"model,omitempty"`
	Scores     map[string]float64 `json:"scores,omitempty"`
	DurationMs int64              `json:"duration_ms,omitempty"`
	Cached     bool               `json:"cached,omitempty"`
}

type (
	autopilotKey   struct{}
	userRequestKey struct{}
)

// WithAutopilot 标记当前 AI 对话临时开启了 Autopilot，优先于资产和分组上的设置。
func WithAutopilot(ctx context.Context) context.Context {
	return context.WithValue(ctx, autopilotKey{}, true)
}

// IsAutopilot 报告当前 AI 对话是否临时开启了 Autopilot。
func IsAutopilot(ctx context.Context) bool {
	v, _ := ctx.Value(autopilotKey{}).(bool)
	return v
}

// WithUserRequest 注入用户在 AI 对话里本轮提出的要求，供模型审核判断命令是否超出要求。
func WithUserRequest(ctx context.Context, text string) context.Context {
	return context.WithValue(ctx, userRequestKey{}, text)
}

// GetUserRequest 获取用户在 AI 对话里本轮提出的要求；不在 AI 对话里时为空。
func GetUserRequest(ctx context.Context) string {
	v, _ := ctx.Value(userRequestKey{}).(string)
	return v
}
