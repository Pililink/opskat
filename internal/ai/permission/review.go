package permission

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/ai/aictx"
	"github.com/opskat/opskat/internal/ai/policy"
	policyent "github.com/opskat/opskat/internal/model/entity/policy"
	"github.com/opskat/opskat/internal/service/asset_svc"
	"github.com/opskat/opskat/internal/service/command_review_svc"
)

// applyReview 按权限模式处理"需要人确认"的结果，不区分资产类型：
//   - 默认：原样返回，问人；
//   - 辅助审批：模型审核通过就放行，否则仍然问人；
//   - Autopilot：模型审核通过就放行，否则直接拒绝，不等人。
//
// 规则已经放行或拒绝的结果原样返回。审核服务没有注册时（未经 bootstrap 的进程）不审核。
func applyReview(ctx context.Context, assetType string, assetID int64, command string, result aictx.CheckResult) aictx.CheckResult {
	if result.Decision != aictx.NeedConfirm {
		return result
	}
	reviewer := command_review_svc.Default()
	if reviewer == nil {
		return result
	}
	mode := resolvePermissionMode(ctx, assetID)
	if mode != policyent.PermissionModeAssisted && mode != policyent.PermissionModeAutopilot {
		return result
	}

	r := reviewer.Review(ctx, command_review_svc.Input{
		AssetType:   assetType,
		Command:     command,
		UserRequest: aictx.GetUserRequest(ctx),
	})
	info := &aictx.ReviewInfo{
		Outcome:    string(r.Outcome),
		Reason:     r.Reason,
		Failed:     r.Failed,
		Model:      r.Model,
		Scores:     r.Scores,
		DurationMs: r.Duration.Milliseconds(),
		Cached:     r.Cached,
	}
	logger.Ctx(ctx).Info("permission review applied",
		zap.Int64("assetID", assetID), zap.String("assetType", assetType),
		zap.String("mode", mode), zap.String("outcome", info.Outcome))

	if r.Outcome == command_review_svc.OutcomePass {
		source := aictx.SourceAssistedAllow
		if mode == policyent.PermissionModeAutopilot {
			source = aictx.SourceAutopilotAllow
		}
		return aictx.CheckResult{Decision: aictx.Allow, DecisionSource: source, Review: info}
	}
	if mode == policyent.PermissionModeAssisted {
		result.Review = info
		return result
	}
	return aictx.CheckResult{
		Decision:       aictx.Deny,
		DecisionSource: aictx.SourceAutopilotDeny,
		Message:        autopilotDenyMessage(ctx, info),
		Review:         info,
	}
}

// resolvePermissionMode 取生效的权限模式：AI 对话临时开启的 Autopilot 优先，
// 其次是资产自己的设置，再沿分组链向上找第一个设置了的；都没有就是默认。
// 资产读取失败时按默认处理（问人），不会因此放宽。
func resolvePermissionMode(ctx context.Context, assetID int64) string {
	if aictx.IsAutopilot(ctx) {
		return policyent.PermissionModeAutopilot
	}
	asset, err := asset_svc.Asset().Get(ctx, assetID)
	if err != nil {
		logger.Ctx(ctx).Warn("get asset for permission mode", zap.Int64("assetID", assetID), zap.Error(err))
		return policyent.PermissionModeDefault
	}
	if asset.PermissionMode != policyent.PermissionModeInherit {
		return asset.PermissionMode
	}
	for _, g := range policy.ResolveGroupChain(ctx, asset.GroupID) {
		if g.PermissionMode != policyent.PermissionModeInherit {
			return g.PermissionMode
		}
	}
	return policyent.PermissionModeDefault
}

// autopilotDenyMessage 是 Autopilot 拒绝时返回给调用方的原因，调用方的 agent 据此决定下一步。
func autopilotDenyMessage(ctx context.Context, info *aictx.ReviewInfo) string {
	if info.Outcome == string(command_review_svc.OutcomeReject) {
		return policy.PolicyFmt(ctx,
			"%s; the command was not executed. Do not retry it as-is: take a safer approach, or leave it for the user to run manually.",
			"%s，命令没有执行。不要原样重试，换一个更安全的做法，或者留给用户手动执行。",
			ReviewSummary(ctx, info))
	}
	return policy.PolicyFmt(ctx,
		"%s; the command was not executed. You may retry later.",
		"%s，命令没有执行，可以稍后重试。",
		ReviewSummary(ctx, info))
}

// ReviewSummary 把审核结果写成一句话，给审批提示和拒绝信息用，例如"模型审核未通过（可能中断服务）"。
func ReviewSummary(ctx context.Context, info *aictx.ReviewInfo) string {
	switch info.Outcome {
	case string(command_review_svc.OutcomePass):
		return policy.PolicyMsg(ctx, "Model review passed", "模型审核通过")
	case string(command_review_svc.OutcomeReject):
		labels := make([]string, 0, len(info.Failed))
		for _, q := range info.Failed {
			labels = append(labels, reviewQuestionLabel(ctx, q))
		}
		return policy.PolicyFmt(ctx, "Model review did not pass (%s)", "模型审核未通过（%s）",
			strings.Join(labels, policy.PolicyMsg(ctx, ", ", "、")))
	default:
		return policy.PolicyFmt(ctx, "Model review failed (%s)", "模型审核失败（%s）", reviewFailReason(ctx, info.Reason))
	}
}

func reviewQuestionLabel(ctx context.Context, question string) string {
	switch question {
	case command_review_svc.QuestionDestructive:
		return policy.PolicyMsg(ctx, "may destroy or irreversibly change data", "可能破坏或不可恢复地修改数据")
	case command_review_svc.QuestionDisruptive:
		return policy.PolicyMsg(ctx, "may interrupt services", "可能中断服务")
	case command_review_svc.QuestionRemoteCode:
		return policy.PolicyMsg(ctx, "downloads and runs code", "会下载并执行代码")
	case command_review_svc.QuestionBeyondRequest:
		return policy.PolicyMsg(ctx, "goes beyond the user's request", "超出了用户的要求")
	default:
		return question
	}
}

func reviewFailReason(ctx context.Context, reason string) string {
	switch reason {
	case command_review_svc.ReasonNotConfigured:
		return policy.PolicyMsg(ctx, "TypeSafe API key is not configured", "未配置 TypeSafe API key")
	case command_review_svc.ReasonInvalidAPIKey:
		return policy.PolicyMsg(ctx, "invalid TypeSafe API key", "TypeSafe API key 无效")
	case command_review_svc.ReasonTimeout:
		return policy.PolicyMsg(ctx, "timed out", "超时")
	case command_review_svc.ReasonTooLong:
		return policy.PolicyMsg(ctx, "command too long", "命令过长")
	default:
		return policy.PolicyMsg(ctx, "service unavailable", "服务不可用")
	}
}
