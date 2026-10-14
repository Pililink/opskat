package aictx

// ReviewInfo 是一次模型审核的结果，随 CheckResult 传到审批界面和审计。
type ReviewInfo struct {
	Mode       string             `json:"mode"`             // 触发审核的权限模式：assisted / autopilot
	Outcome    string             `json:"outcome"`          // pass / reject / fail
	Reason     string             `json:"reason,omitempty"` // 审核失败的原因
	Failed     []string           `json:"failed,omitempty"` // 审核未通过时没满足条件的题目
	Model      string             `json:"model,omitempty"`
	Scores     map[string]float64 `json:"scores,omitempty"`    // 每道题回答"是"的概率
	Threshold  float64            `json:"threshold,omitempty"` // 判断用的阈值
	DurationMs int64              `json:"duration_ms,omitempty"`
	Cached     bool               `json:"cached,omitempty"`
	Attempts   int                `json:"attempts,omitempty"` // 调用模型的次数，超时或连接出错时会重试
	// Extension / Version 是做出判断的审核扩展；内置审核为空。
	Extension string `json:"extension,omitempty"`
	Version   string `json:"version,omitempty"`
	// FailedNames 是审核扩展为未通过的题目声明的名称（题目 id → 语言 → 名称）。
	FailedNames map[string]map[string]string `json:"failed_names,omitempty"`
}
