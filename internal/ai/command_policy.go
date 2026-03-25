package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	"github.com/opskat/opskat/internal/model/entity/grant_entity"
	"github.com/opskat/opskat/internal/model/entity/group_entity"
	"github.com/opskat/opskat/internal/model/entity/policy"
	"github.com/opskat/opskat/internal/repository/grant_repo"
	"github.com/opskat/opskat/internal/repository/group_repo"
	"github.com/opskat/opskat/internal/service/asset_svc"

	"mvdan.cc/sh/v3/syntax"
)

// Decision 权限判定结果
type Decision int

const (
	Allow       Decision = iota // 直接放行
	Deny                        // 拒绝
	NeedConfirm                 // 需要用户确认
)

// 决策来源常量
const (
	SourcePolicyAllow = "policy_allow" // 命令策略白名单放行
	SourcePolicyDeny  = "policy_deny"  // 命令策略黑名单拒绝
	SourceUserAllow   = "user_allow"   // 用户手动允许
	SourceUserDeny    = "user_deny"    // 用户手动拒绝
	SourceGrantAllow  = "grant_allow"  // Grant 预批准匹配放行
	SourceGrantDeny   = "grant_deny"   // Grant 权限申请被拒绝
)

// CheckResult 权限检查结果
type CheckResult struct {
	Decision       Decision
	Message        string   // 返回给 AI 的消息
	HintRules      []string // 拒绝时的允许规则提示
	DecisionSource string   // 决策来源（SourcePolicyAllow 等常量）
	MatchedPattern string   // 匹配的命令模式
}

// DecisionString 返回决策的字符串表示（用于审计日志存储）
func (r CheckResult) DecisionString() string {
	switch r.Decision {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return ""
	}
}

// --- CheckResult context（供 AuditingExecutor 读取决策信息）---

type checkResultKey struct{}

// withCheckResult 注入 CheckResult 占位指针（由 AuditingExecutor 调用）
func withCheckResult(ctx context.Context, r *CheckResult) context.Context {
	return context.WithValue(ctx, checkResultKey{}, r)
}

// setCheckResult 在工具 handler 中设置决策结果
func setCheckResult(ctx context.Context, result CheckResult) {
	if r, ok := ctx.Value(checkResultKey{}).(*CheckResult); ok && r != nil {
		*r = result
	}
}

// CommandConfirmFunc 命令确认回调，阻塞等待用户响应
type CommandConfirmFunc func(assetName, command string) (allowed, alwaysAllow bool)

// GrantRequestFunc Grant 审批回调，创建 grant 并等待用户审批
// patterns 为命令模式列表，用户可能在审批弹窗中编辑
// 返回 (approved, 用户编辑后的 patterns)
type GrantRequestFunc func(assetID int64, assetName string, patterns []string, reason string) (approved bool, finalPatterns []string)

// ApprovedPattern 会话级已批准的命令模式
type ApprovedPattern struct {
	AssetID int64  // 0 表示所有资产
	Pattern string // 命令模式，支持 * 通配，复用 MatchCommandRule 匹配
}

// Match 检查实际命令是否匹配此模式
func (p *ApprovedPattern) Match(assetID int64, command string) bool {
	if p.AssetID != 0 && p.AssetID != assetID {
		return false
	}
	return MatchCommandRule(p.Pattern, command)
}

// CommandPolicyChecker 命令权限检查器，通过 context 注入到两条执行路径
type CommandPolicyChecker struct {
	confirmFunc      CommandConfirmFunc
	grantRequestFunc GrantRequestFunc
	grantAllowed     []ApprovedPattern // Grant 预批准白名单（资产+命令模式）
	mu               sync.Mutex
}

// NewCommandPolicyChecker 创建权限检查器
func NewCommandPolicyChecker(confirmFunc CommandConfirmFunc) *CommandPolicyChecker {
	return &CommandPolicyChecker{
		confirmFunc: confirmFunc,
	}
}

// SetGrantRequestFunc 设置 Grant 审批回调
func (c *CommandPolicyChecker) SetGrantRequestFunc(fn GrantRequestFunc) {
	c.grantRequestFunc = fn
}

// SubmitGrant 提交 grant 审批请求（request_permission 工具调用）
func (c *CommandPolicyChecker) SubmitGrant(ctx context.Context, assetID int64, patterns []string, reason string) CheckResult {
	if c.grantRequestFunc == nil {
		return CheckResult{Decision: Deny, Message: "无 Grant 审批机制"}
	}

	assetName := ""
	if assetID > 0 {
		asset, err := asset_svc.Asset().Get(ctx, assetID)
		if err != nil {
			logger.Default().Warn("get asset for grant submission", zap.Int64("assetID", assetID), zap.Error(err))
		}
		if asset != nil {
			assetName = asset.Name
		}
	}

	approved, finalPatterns := c.grantRequestFunc(assetID, assetName, patterns, reason)
	if !approved {
		return CheckResult{Decision: Deny, Message: "用户拒绝 Grant 审批", DecisionSource: SourceGrantDeny, MatchedPattern: strings.Join(patterns, "; ")}
	}

	return CheckResult{Decision: Allow, Message: fmt.Sprintf("Grant 已批准，共 %d 条模式", len(finalPatterns)), DecisionSource: SourceGrantAllow, MatchedPattern: strings.Join(finalPatterns, "; ")}
}

// AddApprovedPattern 添加已批准的模式（外部调用，如 opsctl 审批后）
func (c *CommandPolicyChecker) AddApprovedPattern(assetID int64, pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grantAllowed = append(c.grantAllowed, ApprovedPattern{
		AssetID: assetID,
		Pattern: pattern,
	})
}

// matchGrantPatterns 从 DB 中查找已批准 grant 的 items，用通配匹配命令
// 返回首个匹配的 pattern，空字符串表示未匹配
// groups 为资产所属的组链（组 → 父组 → ... → 根）
func matchGrantPatterns(ctx context.Context, assetID int64, groups []*group_entity.Group, subCmds []string) string {
	sessionID := GetSessionID(ctx)
	if sessionID == "" {
		return ""
	}
	repo := grant_repo.Grant()
	if repo == nil {
		return ""
	}
	items, err := repo.ListApprovedItems(ctx, sessionID)
	if err != nil || len(items) == 0 {
		return ""
	}

	// 构建资产所属的组 ID 集合，用于匹配 group 级 grant item
	groupIDs := make(map[int64]bool, len(groups))
	for _, g := range groups {
		groupIDs[g.ID] = true
	}

	// 所有子命令都必须匹配某个 grant item
	var firstPattern string
	for _, cmd := range subCmds {
		matched := false
		for _, item := range items {
			if !grantItemMatchesTarget(item, assetID, groupIDs) {
				continue
			}
			if MatchCommandRule(item.Command, cmd) {
				matched = true
				if firstPattern == "" {
					firstPattern = item.Command
				}
				break
			}
		}
		if !matched {
			return ""
		}
	}
	return firstPattern
}

// grantItemMatchesTarget 检查 grant item 是否匹配目标资产
// AssetID=0 且 GroupID=0 → 匹配所有资产
// AssetID>0 → 精确匹配资产
// GroupID>0 → 匹配组内资产（检查资产所属组链）
func grantItemMatchesTarget(item *grant_entity.GrantItem, assetID int64, groupIDs map[int64]bool) bool {
	if item.AssetID != 0 {
		return item.AssetID == assetID
	}
	if item.GroupID != 0 {
		return groupIDs[item.GroupID]
	}
	// AssetID=0 且 GroupID=0，匹配所有资产
	return true
}

// matchSessionPatterns 检查所有子命令是否都能被会话级模式匹配（调用方需持有 mu 锁）
// 返回是否匹配，以及首个匹配的模式（用于审计）
func (c *CommandPolicyChecker) matchSessionPatterns(assetID int64, subCmds []string) (bool, string) {
	if len(c.grantAllowed) == 0 {
		return false, ""
	}
	var firstPattern string
	for _, cmd := range subCmds {
		matched := false
		for i := range c.grantAllowed {
			if c.grantAllowed[i].Match(assetID, cmd) {
				matched = true
				if firstPattern == "" {
					firstPattern = c.grantAllowed[i].Pattern
				}
				break
			}
		}
		if !matched {
			return false, ""
		}
	}
	return true, firstPattern
}

// Reset 重置会话级白名单
func (c *CommandPolicyChecker) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grantAllowed = nil
}

// Check 检查命令是否允许执行
func (c *CommandPolicyChecker) Check(ctx context.Context, assetID int64, command string) CheckResult {
	// 1. 提取所有子命令
	subCmds, err := ExtractSubCommands(command)
	if err != nil || len(subCmds) == 0 {
		// 解析失败，整条视为一个命令
		subCmds = []string{command}
	}

	// 2. 获取资产 + 组链
	asset, err := asset_svc.Asset().Get(ctx, assetID)
	if err != nil {
		logger.Default().Warn("get asset for command check", zap.Int64("assetID", assetID), zap.Error(err))
	}
	var groups []*group_entity.Group
	if asset != nil && asset.GroupID > 0 {
		groups = resolveGroupChain(ctx, asset.GroupID)
	}

	// 收集所有层级的策略（含权限组解析）
	allPolicies := collectPolicies(ctx, asset, groups)
	allDenyRules := collectDenyRules(allPolicies)
	allAllowRules := collectAllowRules(allPolicies)

	// 3. 检查 deny list（所有层级合并，任一匹配即拒绝）
	for _, cmd := range subCmds {
		for _, rule := range allDenyRules {
			if MatchCommandRule(rule, cmd) {
				assetName := ""
				if asset != nil {
					assetName = asset.Name
				}
				hints := findHintRules(cmd, allAllowRules)
				msg := formatDenyMessage(assetName, command, "命令被策略禁止执行", hints)
				return CheckResult{Decision: Deny, Message: msg, HintRules: hints, DecisionSource: SourcePolicyDeny, MatchedPattern: rule}
			}
		}
	}

	// 4. 检查 allow list（所有子命令都匹配才放行）
	if len(allAllowRules) > 0 {
		if ok, matched := allSubCommandsAllowed(subCmds, allAllowRules); ok {
			return CheckResult{Decision: Allow, DecisionSource: SourcePolicyAllow, MatchedPattern: matched}
		}
	}

	// 5. 检查会话级白名单（按 assetID + pattern 匹配，exec 弹窗的「始终允许」）
	c.mu.Lock()
	sessionOK, matchedPattern := c.matchSessionPatterns(assetID, subCmds)
	c.mu.Unlock()
	if sessionOK {
		return CheckResult{Decision: Allow, DecisionSource: SourceGrantAllow, MatchedPattern: matchedPattern}
	}

	// 6. 检查 Grant 预批准（DB 中已批准的 grant items 做通配匹配）
	if grantPattern := matchGrantPatterns(ctx, assetID, groups, subCmds); grantPattern != "" {
		return CheckResult{Decision: Allow, DecisionSource: SourceGrantAllow, MatchedPattern: grantPattern}
	}

	// 7. 请求用户确认
	if c.confirmFunc == nil {
		hints := findHintRules(subCmds[0], allAllowRules)
		msg := formatDenyMessage("", command, "命令未授权且无确认机制", hints)
		return CheckResult{Decision: Deny, Message: msg, HintRules: hints, DecisionSource: SourcePolicyDeny}
	}

	assetName := ""
	if asset != nil {
		assetName = asset.Name
	}
	allowed, alwaysAllow := c.confirmFunc(assetName, command)
	if !allowed {
		hints := findHintRules(subCmds[0], allAllowRules)
		msg := formatDenyMessage(assetName, command, "用户拒绝执行", hints)
		return CheckResult{Decision: Deny, Message: msg, HintRules: hints, DecisionSource: SourceUserDeny}
	}

	// "始终允许" → 每个子命令加入会话白名单（绑定资产ID）
	if alwaysAllow {
		c.mu.Lock()
		for _, cmd := range subCmds {
			c.grantAllowed = append(c.grantAllowed, ApprovedPattern{
				AssetID: assetID,
				Pattern: cmd,
			})
		}
		c.mu.Unlock()
		// 写审计日志记录会话模式变更
		writeGrantSubmitAudit(ctx, assetID, assetName, subCmds)
	}

	return CheckResult{Decision: Allow, DecisionSource: SourceUserAllow}
}

// CheckPolicyOnly 只检查 allow/deny 列表，不触发确认回调。
// 返回 Allow（允许列表匹配）、Deny（拒绝列表匹配）或 NeedConfirm（未匹配任何列表）。
func CheckPolicyOnly(ctx context.Context, assetID int64, command string) CheckResult {
	subCmds, err := ExtractSubCommands(command)
	if err != nil || len(subCmds) == 0 {
		subCmds = []string{command}
	}

	asset, err := asset_svc.Asset().Get(ctx, assetID)
	if err != nil {
		logger.Default().Warn("get asset for policy check", zap.Int64("assetID", assetID), zap.Error(err))
	}
	var groups []*group_entity.Group
	if asset != nil && asset.GroupID > 0 {
		groups = resolveGroupChain(ctx, asset.GroupID)
	}

	allPolicies := collectPolicies(ctx, asset, groups)
	allDenyRules := collectDenyRules(allPolicies)
	allAllowRules := collectAllowRules(allPolicies)

	// Check deny list
	for _, cmd := range subCmds {
		for _, rule := range allDenyRules {
			if MatchCommandRule(rule, cmd) {
				assetName := ""
				if asset != nil {
					assetName = asset.Name
				}
				hints := findHintRules(cmd, allAllowRules)
				msg := formatDenyMessage(assetName, command, "命令被策略禁止执行", hints)
				return CheckResult{Decision: Deny, Message: msg, HintRules: hints, DecisionSource: SourcePolicyDeny, MatchedPattern: rule}
			}
		}
	}

	// Check allow list
	if len(allAllowRules) > 0 {
		if ok, matched := allSubCommandsAllowed(subCmds, allAllowRules); ok {
			return CheckResult{Decision: Allow, DecisionSource: SourcePolicyAllow, MatchedPattern: matched}
		}
	}

	return CheckResult{Decision: NeedConfirm, HintRules: allAllowRules}
}

// CheckSQLPolicyForOpsctl 检查 SQL 策略（无确认回调），用于 opsctl 前置策略检查。
// NeedConfirm 时 HintRules 包含允许的 SQL 类型。
func CheckSQLPolicyForOpsctl(ctx context.Context, assetID int64, sqlText string) CheckResult {
	// 1. 组通用策略（CmdPolicy）
	groupResult := CheckGroupGenericPolicy(ctx, assetID, sqlText, MatchCommandRule)
	if groupResult.Decision == Deny {
		return groupResult
	}

	// 2. SQL 分类 + 查询策略
	stmts, err := ClassifyStatements(sqlText)
	if err != nil {
		return CheckResult{Decision: Deny, Message: fmt.Sprintf("SQL 解析失败，拒绝执行: %v", err)}
	}

	asset, _ := resolveAssetPolicyChain(ctx, assetID)
	mergedPolicy := collectQueryPolicies(ctx, asset)
	result := CheckQueryPolicy(mergedPolicy, stmts)

	// 组通用 allow 优先于类型专用的 NeedConfirm
	if result.Decision == NeedConfirm && groupResult.Decision == Allow {
		return groupResult
	}

	// NeedConfirm 时收集允许的 SQL 类型作为提示
	if result.Decision == NeedConfirm {
		merged := mergeQueryPolicy(mergedPolicy, asset_entity.DefaultQueryPolicy())
		if len(merged.AllowTypes) > 0 {
			result.HintRules = merged.AllowTypes
		}
	}

	return result
}

// CheckRedisPolicyForOpsctl 检查 Redis 策略（无确认回调），用于 opsctl 前置策略检查。
// NeedConfirm 时 HintRules 包含允许的 Redis 命令。
func CheckRedisPolicyForOpsctl(ctx context.Context, assetID int64, command string) CheckResult {
	// 1. 组通用策略（CmdPolicy）
	groupResult := CheckGroupGenericPolicy(ctx, assetID, command, MatchRedisRule)
	if groupResult.Decision == Deny {
		return groupResult
	}

	// 2. Redis 策略
	asset, _ := resolveAssetPolicyChain(ctx, assetID)
	mergedPolicy := collectRedisPolicies(ctx, asset)
	result := CheckRedisPolicy(mergedPolicy, command)

	// 组通用 allow 优先于类型专用的 NeedConfirm
	if result.Decision == NeedConfirm && groupResult.Decision == Allow {
		return groupResult
	}

	// NeedConfirm 时收集允许的 Redis 命令作为提示
	if result.Decision == NeedConfirm {
		merged := mergeRedisPolicy(mergedPolicy, asset_entity.DefaultRedisPolicy())
		if len(merged.AllowList) > 0 {
			result.HintRules = merged.AllowList
		}
	}

	return result
}

// CheckForAsset 按资产类型分发权限检查
func (c *CommandPolicyChecker) CheckForAsset(ctx context.Context, assetID int64, assetType, command string) CheckResult {
	switch assetType {
	case asset_entity.AssetTypeSSH:
		return c.Check(ctx, assetID, command)

	case asset_entity.AssetTypeDatabase:
		// 先检查组通用策略（CmdPolicy，用 MatchCommandRule）
		groupResult := CheckGroupGenericPolicy(ctx, assetID, command, MatchCommandRule)
		if groupResult.Decision == Deny {
			return groupResult
		}
		stmts, err := ClassifyStatements(command)
		if err != nil {
			return CheckResult{Decision: Deny, Message: fmt.Sprintf("SQL 解析失败，拒绝执行: %v", err)}
		}
		asset, _ := resolveAssetPolicyChain(ctx, assetID)
		mergedPolicy := collectQueryPolicies(ctx, asset)
		result := CheckQueryPolicy(mergedPolicy, stmts)
		// 组通用 allow 优先于类型专用的 NeedConfirm
		if result.Decision == NeedConfirm && groupResult.Decision == Allow {
			return groupResult
		}
		if result.Decision == NeedConfirm {
			return c.handleConfirm(ctx, assetID, asset, command)
		}
		return result

	case asset_entity.AssetTypeRedis:
		// 先检查组通用策略（CmdPolicy，用 MatchRedisRule）
		groupResult := CheckGroupGenericPolicy(ctx, assetID, command, MatchRedisRule)
		if groupResult.Decision == Deny {
			return groupResult
		}
		asset, _ := resolveAssetPolicyChain(ctx, assetID)
		mergedPolicy := collectRedisPolicies(ctx, asset)
		result := CheckRedisPolicy(mergedPolicy, command)
		// 组通用 allow 优先于类型专用的 NeedConfirm
		if result.Decision == NeedConfirm && groupResult.Decision == Allow {
			return groupResult
		}
		if result.Decision == NeedConfirm {
			return c.handleConfirm(ctx, assetID, asset, command)
		}
		return result
	}
	return CheckResult{Decision: NeedConfirm}
}

// handleConfirm 处理需要用户确认的情况
func (c *CommandPolicyChecker) handleConfirm(ctx context.Context, assetID int64, asset *asset_entity.Asset, command string) CheckResult {
	// 检查会话级白名单
	c.mu.Lock()
	sessionOK, matchedPattern := c.matchSessionPatterns(assetID, []string{command})
	c.mu.Unlock()
	if sessionOK {
		return CheckResult{Decision: Allow, DecisionSource: SourceGrantAllow, MatchedPattern: matchedPattern}
	}

	if c.confirmFunc == nil {
		return CheckResult{Decision: Deny, Message: "命令未授权且无确认机制", DecisionSource: SourcePolicyDeny}
	}

	assetName := ""
	if asset != nil {
		assetName = asset.Name
	}
	allowed, alwaysAllow := c.confirmFunc(assetName, command)
	if !allowed {
		return CheckResult{Decision: Deny, Message: fmt.Sprintf("用户拒绝执行: %s", command), DecisionSource: SourceUserDeny}
	}
	if alwaysAllow {
		c.mu.Lock()
		c.grantAllowed = append(c.grantAllowed, ApprovedPattern{
			AssetID: assetID,
			Pattern: command,
		})
		c.mu.Unlock()
	}
	return CheckResult{Decision: Allow, DecisionSource: SourceUserAllow}
}

// --- context 注入 ---

type policyCheckerKeyType struct{}

// WithPolicyChecker 将 PolicyChecker 注入 context
func WithPolicyChecker(ctx context.Context, c *CommandPolicyChecker) context.Context {
	return context.WithValue(ctx, policyCheckerKeyType{}, c)
}

// GetPolicyChecker 从 context 中获取 PolicyChecker
func GetPolicyChecker(ctx context.Context) *CommandPolicyChecker {
	c, _ := ctx.Value(policyCheckerKeyType{}).(*CommandPolicyChecker)
	return c
}

// --- Shell AST 解析 ---

// ExtractSubCommands 从 shell 命令中提取所有子命令（处理 &&、||、;、|、$() 等）
func ExtractSubCommands(command string) ([]string, error) {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, fmt.Errorf("shell 解析失败: %w", err)
	}

	var cmds []string
	printer := syntax.NewPrinter()

	var extractFromStmt func(stmt *syntax.Stmt)
	extractFromStmt = func(stmt *syntax.Stmt) {
		if stmt == nil || stmt.Cmd == nil {
			return
		}
		switch cmd := stmt.Cmd.(type) {
		case *syntax.BinaryCmd:
			// &&、||、| 等二元操作
			extractFromStmt(cmd.X)
			extractFromStmt(cmd.Y)
		default:
			// CallExpr、其他命令类型 — 打印为字符串
			var buf strings.Builder
			if err := printer.Print(&buf, stmt.Cmd); err != nil {
				logger.Default().Warn("print shell statement", zap.Error(err))
			}
			cmdStr := strings.TrimSpace(buf.String())
			if cmdStr != "" {
				cmds = append(cmds, cmdStr)
			}
		}
	}

	syntax.Walk(file, func(node syntax.Node) bool {
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}
		extractFromStmt(stmt)
		return false
	})

	return cmds, nil
}

// --- 命令规则匹配 ---

// ParsedCommand 解析后的命令结构
type ParsedCommand struct {
	Program     string
	SubCommands []string
	Flags       map[string]string
	Wildcard    bool
}

// ParseCommandRule 将规则字符串解析为结构化表示
func ParseCommandRule(rule string) *ParsedCommand {
	tokens := tokenize(rule)
	if len(tokens) == 0 {
		return &ParsedCommand{}
	}

	result := &ParsedCommand{
		Program: tokens[0],
		Flags:   make(map[string]string),
	}

	i := 1
	for i < len(tokens) {
		t := tokens[i]
		if isFlag(t) {
			if strings.Contains(t, "=") {
				// --flag=value
				parts := strings.SplitN(t, "=", 2)
				result.Flags[parts[0]] = parts[1]
			} else if i+1 < len(tokens) && !isFlag(tokens[i+1]) {
				// -f value（* 在 flag 后面作为值，不是通配符）
				result.Flags[t] = tokens[i+1]
				i++
			} else {
				// 布尔 flag
				result.Flags[t] = ""
			}
		} else if t == "*" {
			// 只有非 flag 值位置的 * 才是通配符
			result.Wildcard = true
		} else {
			result.SubCommands = append(result.SubCommands, t)
		}
		i++
	}

	return result
}

// ParseActualCommand 解析实际命令，用规则的 flag 列表作为参照判断哪些 flag 带值
func ParseActualCommand(command string, rule *ParsedCommand) *ParsedCommand {
	tokens := tokenize(command)
	if len(tokens) == 0 {
		return &ParsedCommand{}
	}

	result := &ParsedCommand{
		Program: tokens[0],
		Flags:   make(map[string]string),
	}

	i := 1
	for i < len(tokens) {
		t := tokens[i]
		if isFlag(t) {
			if strings.Contains(t, "=") {
				parts := strings.SplitN(t, "=", 2)
				result.Flags[parts[0]] = parts[1]
			} else if i+1 < len(tokens) && !isFlag(tokens[i+1]) {
				// 用规则判断：如果规则中该 flag 带值，则实际命令中也视为带值
				if _, hasValue := rule.Flags[t]; hasValue || rule.Flags[t] != "" {
					result.Flags[t] = tokens[i+1]
					i++
				} else {
					// 规则中没有该 flag，按启发式处理：
					// 如果下一个 token 不是 flag 且不以 - 开头，视为带值
					result.Flags[t] = tokens[i+1]
					i++
				}
			} else {
				result.Flags[t] = ""
			}
		} else {
			result.SubCommands = append(result.SubCommands, t)
		}
		i++
	}

	return result
}

// MatchCommandRule 检查实际命令是否匹配规则字符串
func MatchCommandRule(rule, command string) bool {
	parsedRule := ParseCommandRule(rule)
	if parsedRule.Program == "" {
		return false
	}

	parsedCmd := ParseActualCommand(command, parsedRule)
	if parsedCmd.Program == "" {
		return false
	}

	// 1. 程序名必须相同
	if parsedRule.Program != parsedCmd.Program {
		return false
	}

	// 2. 规则中所有子命令必须出现（顺序无关）
	for _, sub := range parsedRule.SubCommands {
		if !matchSubCommand(sub, parsedCmd.SubCommands) {
			return false
		}
	}

	// 3. 规则中所有 flag 必须匹配
	for flag, ruleVal := range parsedRule.Flags {
		actualVal, ok := parsedCmd.Flags[flag]
		if !ok {
			return false
		}
		if ruleVal != "" && ruleVal != "*" && !matchGlobPattern(ruleVal, actualVal) {
			return false
		}
	}

	// 4. 无通配符时，不允许多余子命令和多余 flag
	if !parsedRule.Wildcard {
		if len(parsedCmd.SubCommands) > len(parsedRule.SubCommands) {
			return false
		}
		// 检查是否有规则中未定义的 flag
		for flag := range parsedCmd.Flags {
			if _, ok := parsedRule.Flags[flag]; !ok {
				return false
			}
		}
	}

	return true
}

// --- 辅助函数 ---

func tokenize(s string) []string {
	fields := strings.Fields(s)
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		result = append(result, expandShortFlag(f)...)
	}
	return result
}

// expandShortFlag 展开组合短 flag（如 -rf → -r, -f）
// 不展开：单字符 flag（-n）、长 flag（--verbose）、含 = 的 flag（-n=val）、非 flag
func expandShortFlag(token string) []string {
	if !strings.HasPrefix(token, "-") || strings.HasPrefix(token, "--") {
		return []string{token}
	}
	chars := token[1:]
	if len(chars) <= 1 || strings.Contains(token, "=") {
		return []string{token}
	}
	result := make([]string, len(chars))
	for i, c := range chars {
		result[i] = "-" + string(c)
	}
	return result
}

func isFlag(s string) bool {
	return strings.HasPrefix(s, "-")
}

func matchSubCommand(pattern string, subs []string) bool {
	for _, sub := range subs {
		if matchGlobPattern(pattern, sub) {
			return true
		}
	}
	return false
}

// matchGlobPattern 使用 filepath.Match 做 glob 匹配
func matchGlobPattern(pattern, value string) bool {
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return pattern == value
	}
	return matched
}

// allSubCommandsAllowed 检查所有子命令是否都匹配 allow 规则，返回是否全部匹配及命中的规则
func allSubCommandsAllowed(subCmds []string, allowRules []string) (bool, string) {
	if len(allowRules) == 0 {
		return false, ""
	}
	matchedRules := make(map[string]struct{})
	for _, cmd := range subCmds {
		matched := false
		for _, rule := range allowRules {
			if MatchCommandRule(rule, cmd) {
				matched = true
				matchedRules[rule] = struct{}{}
				break
			}
		}
		if !matched {
			return false, ""
		}
	}
	// 收集去重的匹配规则
	rules := make([]string, 0, len(matchedRules))
	for r := range matchedRules {
		rules = append(rules, r)
	}
	return true, strings.Join(rules, ", ")
}

// findHintRules 从 allow 规则中找同程序名的规则作为提示
func findHintRules(command string, allowRules []string) []string {
	tokens := tokenize(command)
	if len(tokens) == 0 {
		return nil
	}
	program := tokens[0]

	var hints []string
	for _, rule := range allowRules {
		ruleTokens := tokenize(rule)
		if len(ruleTokens) > 0 && ruleTokens[0] == program {
			hints = append(hints, rule)
		}
	}
	return hints
}

func formatDenyMessage(assetName, command, reason string, hints []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "命令执行被拒绝（%s）。\n命令: %s", reason, command)
	if len(hints) > 0 {
		sb.WriteString("\n\n该资产允许的相关命令格式：\n")
		for _, h := range hints {
			fmt.Fprintf(&sb, "- %s\n", h)
		}
		sb.WriteString("\n请按照上述格式调整命令后重试。")
	}
	return sb.String()
}

// --- 策略收集 ---

// resolveAssetPolicyChain 解析资产及其组链，返回按优先级排序的策略持有者列表（资产优先）
func resolveAssetPolicyChain(ctx context.Context, assetID int64) (*asset_entity.Asset, []policy.Holder) {
	asset, err := asset_svc.Asset().Get(ctx, assetID)
	if err != nil {
		logger.Default().Warn("get asset for policy check", zap.Int64("assetID", assetID), zap.Error(err))
		return nil, nil
	}
	var holders []policy.Holder
	holders = append(holders, asset)
	if asset.GroupID > 0 {
		for _, g := range resolveGroupChain(ctx, asset.GroupID) {
			holders = append(holders, g)
		}
	}
	return asset, holders
}

// collectPoliciesFromChain 从策略链中收集指定类型的策略
func collectPoliciesFromChain[T any](holders []policy.Holder, getter func(policy.Holder) (*T, error)) []*T {
	var policies []*T
	for _, h := range holders {
		if p, err := getter(h); err == nil && p != nil {
			policies = append(policies, p)
		}
	}
	return policies
}

func collectPolicies(ctx context.Context, asset *asset_entity.Asset, groups []*group_entity.Group) []*asset_entity.CommandPolicy {
	var holders []policy.Holder
	if asset != nil {
		holders = append(holders, asset)
	}
	for _, g := range groups {
		holders = append(holders, g)
	}
	policies := collectPoliciesFromChain(holders, func(h policy.Holder) (*asset_entity.CommandPolicy, error) {
		return h.GetCommandPolicy()
	})
	// 解析每个策略引用的权限组，将组的规则合并进来
	for _, p := range policies {
		if len(p.Groups) > 0 {
			grpAllow, grpDeny := resolveCommandGroups(ctx, p.Groups)
			p.AllowList = append(p.AllowList, grpAllow...)
			p.DenyList = append(p.DenyList, grpDeny...)
		}
	}
	return policies
}

// collectQueryPolicies 收集资产 + 组链的 SQL 权限策略并合并
func collectQueryPolicies(ctx context.Context, asset *asset_entity.Asset) *asset_entity.QueryPolicy {
	var holders []policy.Holder
	if asset != nil {
		holders = append(holders, asset)
		if asset.GroupID > 0 {
			for _, g := range resolveGroupChain(ctx, asset.GroupID) {
				holders = append(holders, g)
			}
		}
	}
	policies := collectPoliciesFromChain(holders, func(h policy.Holder) (*asset_entity.QueryPolicy, error) {
		return h.GetQueryPolicy()
	})
	if len(policies) == 0 {
		return nil
	}
	// 解析引用的权限组
	for _, p := range policies {
		if len(p.Groups) > 0 {
			grpAllowTypes, grpDenyTypes, grpDenyFlags := resolveQueryGroups(ctx, p.Groups)
			p.AllowTypes = append(p.AllowTypes, grpAllowTypes...)
			p.DenyTypes = append(p.DenyTypes, grpDenyTypes...)
			p.DenyFlags = append(p.DenyFlags, grpDenyFlags...)
		}
	}
	// 合并：allow_types 取第一个非空（资产优先），deny_types/deny_flags 全部合并
	merged := &asset_entity.QueryPolicy{}
	for _, p := range policies {
		if len(merged.AllowTypes) == 0 && len(p.AllowTypes) > 0 {
			merged.AllowTypes = p.AllowTypes
		}
		merged.DenyTypes = appendUnique(merged.DenyTypes, p.DenyTypes...)
		merged.DenyFlags = appendUnique(merged.DenyFlags, p.DenyFlags...)
	}
	return merged
}

// collectRedisPolicies 收集资产 + 组链的 Redis 权限策略并合并
func collectRedisPolicies(ctx context.Context, asset *asset_entity.Asset) *asset_entity.RedisPolicy {
	var holders []policy.Holder
	if asset != nil {
		holders = append(holders, asset)
		if asset.GroupID > 0 {
			for _, g := range resolveGroupChain(ctx, asset.GroupID) {
				holders = append(holders, g)
			}
		}
	}
	policies := collectPoliciesFromChain(holders, func(h policy.Holder) (*asset_entity.RedisPolicy, error) {
		return h.GetRedisPolicy()
	})
	if len(policies) == 0 {
		return nil
	}
	// 解析引用的权限组
	for _, p := range policies {
		if len(p.Groups) > 0 {
			grpAllow, grpDeny := resolveRedisGroups(ctx, p.Groups)
			p.AllowList = append(p.AllowList, grpAllow...)
			p.DenyList = append(p.DenyList, grpDeny...)
		}
	}
	// 合并：allow_list 取第一个非空（资产优先），deny_list 全部合并
	merged := &asset_entity.RedisPolicy{}
	for _, p := range policies {
		if len(merged.AllowList) == 0 && len(p.AllowList) > 0 {
			merged.AllowList = p.AllowList
		}
		merged.DenyList = appendUnique(merged.DenyList, p.DenyList...)
	}
	return merged
}

func collectDenyRules(policies []*asset_entity.CommandPolicy) []string {
	rules := make([]string, 0, len(policies))
	for _, p := range policies {
		rules = append(rules, p.DenyList...)
	}
	return rules
}

func collectAllowRules(policies []*asset_entity.CommandPolicy) []string {
	rules := make([]string, 0, len(policies))
	for _, p := range policies {
		rules = append(rules, p.AllowList...)
	}
	return rules
}

// resolveGroupChain 递归获取组链（组 → 父组 → ... → 根），最大深度 5
func resolveGroupChain(ctx context.Context, groupID int64) []*group_entity.Group {
	var chain []*group_entity.Group
	currentID := groupID
	for i := 0; i < 5 && currentID > 0; i++ {
		g, err := group_repo.Group().Find(ctx, currentID)
		if err != nil {
			break
		}
		chain = append(chain, g)
		currentID = g.ParentID
	}
	return chain
}
