package permission

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/ai/aictx"
	"github.com/opskat/opskat/internal/ai/policy"
	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	policyent "github.com/opskat/opskat/internal/model/entity/policy"
	"github.com/opskat/opskat/internal/service/asset_svc"
	"github.com/opskat/opskat/internal/service/command_review_svc"
)

// applyReviews 按权限模式就地处理 results 里"需要人确认"的结果，不区分资产类型：
//   - 默认：原样保留，问人；
//   - 辅助审批：模型审核通过就放行，否则仍然问人；
//   - Autopilot：模型审核通过就放行，否则直接拒绝，不等人。
//
// 规则已经放行或拒绝的结果原样保留。需要审核的命令一次交给 ReviewBatch。
// 审核服务没有注册时（未经 bootstrap 的进程）不审核。
func applyReviews(ctx context.Context, reqs []PermissionRequest, results []aictx.CheckResult) {
	reviewer := command_review_svc.Default()
	if reviewer == nil {
		return
	}
	var idx []int
	var modes []string
	var inputs []command_review_svc.Input
	for i, r := range results {
		if r.Decision != aictx.NeedConfirm {
			continue
		}
		mode := resolvePermissionMode(ctx, reqs[i].AssetID)
		if mode != policyent.PermissionModeAssisted && mode != policyent.PermissionModeAutopilot {
			continue
		}
		idx = append(idx, i)
		modes = append(modes, mode)
		inputs = append(inputs, command_review_svc.Input{AssetType: reqs[i].AssetType, Command: reqs[i].Command})
	}
	if len(inputs) == 0 {
		return
	}
	for k, r := range reviewer.ReviewBatch(ctx, inputs) {
		i := idx[k]
		results[i] = applyReview(ctx, reqs[i], modes[k], results[i], r)
	}
}

// applyReview 把一条审核结果按模式落成权限结果。
func applyReview(ctx context.Context, req PermissionRequest, mode string, result aictx.CheckResult, r command_review_svc.Result) aictx.CheckResult {
	info := &aictx.ReviewInfo{
		Mode:       mode,
		Outcome:    string(r.Outcome),
		Reason:     r.Reason,
		Failed:     r.Failed,
		Model:      r.Model,
		Scores:     r.Scores,
		Threshold:  r.Threshold,
		DurationMs: r.Duration.Milliseconds(),
		Cached:     r.Cached,
	}
	logger.Ctx(ctx).Info("permission review applied",
		zap.Int64("assetID", req.AssetID), zap.String("assetType", req.AssetType),
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

// resolvePermissionMode 取生效的权限模式：资产自己的设置优先，没设置时沿分组链向上找
// 第一个设置了的；都没有就是默认。资产读取失败时按默认处理（问人），不会因此放宽。
func resolvePermissionMode(ctx context.Context, assetID int64) string {
	asset, err := asset_svc.Asset().Get(ctx, assetID)
	if err != nil {
		logger.Ctx(ctx).Warn("get asset for permission mode", zap.Int64("assetID", assetID), zap.Error(err))
		return policyent.PermissionModeDefault
	}
	return permissionModeOf(ctx, asset)
}

// splitAutopilotGrants 把授权申请里 Autopilot 资产的部分分出来，返回其余部分和这些资产的名字。
// Autopilot 是无人值守，不会有人来批；批准的又是通配模式，会让之后匹配的命令跳过逐条审核，
// 所以这些资产不接受授权申请，由调用方直接执行、模型逐条审核。
func splitAutopilotGrants(ctx context.Context, items []GrantItem) (rest []GrantItem, autopilot []string) {
	for _, item := range items {
		if item.AssetID > 0 {
			asset, err := asset_svc.Asset().Get(ctx, item.AssetID)
			if err != nil {
				logger.Ctx(ctx).Warn("get asset for grant permission mode", zap.Int64("assetID", item.AssetID), zap.Error(err))
			} else if permissionModeOf(ctx, asset) == policyent.PermissionModeAutopilot {
				autopilot = append(autopilot, asset.Name)
				continue
			}
		}
		rest = append(rest, item)
	}
	return rest, autopilot
}

// autopilotGrantMessage 告诉调用方这些资产不走授权申请，该怎么做。
func autopilotGrantMessage(ctx context.Context, assets []string) string {
	return policy.PolicyFmt(ctx,
		"Asset(s) %s use Autopilot: grant requests are not accepted there and not needed, and nothing was granted for them. Run the commands directly; each one is reviewed by the model. Do not use a grant request to get around a command the review refused; leave that command to the user.",
		"资产 %s 使用 Autopilot：不接受授权申请，也不需要，没有为它授予任何权限。请直接执行命令，每条命令会由模型单独审核；被审核拒绝的命令不要改用授权申请绕过，交给用户处理。",
		strings.Join(assets, ", "))
}

// permissionModeOf 是 resolvePermissionMode 在已经拿到资产时的版本。
func permissionModeOf(ctx context.Context, asset *asset_entity.Asset) string {
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
	default:
		return question
	}
}

func reviewFailReason(ctx context.Context, reason string) string {
	switch reason {
	case command_review_svc.ReasonNotConfigured:
		return policy.PolicyMsg(ctx, "the command review API key is not configured", "未配置命令审核的 API key")
	case command_review_svc.ReasonInvalidAPIKey:
		return policy.PolicyMsg(ctx, "the command review API key is invalid", "命令审核的 API key 无效")
	case command_review_svc.ReasonTimeout:
		return policy.PolicyMsg(ctx, "timed out", "超时")
	case command_review_svc.ReasonTooLong:
		return policy.PolicyMsg(ctx, "command too long", "命令过长")
	default:
		return policy.PolicyMsg(ctx, "service unavailable", "服务不可用")
	}
}
