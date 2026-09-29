# 辅助审批与 Autopilot：用 Jev 模型审核命令

> Status: In progress（后端已实现，前端待做）
> Owner: OpsKat maintainers
> Last updated: 2026-09-29
> Issue: #327

**Objective:** 命令没被规则放行、原本要问人时，先让 TypeSafe 的 Jev 模型审核一次。每台服务器（或服务器分组）选一种权限模式：

| 模式 | 审核通过 | 审核未通过 / 审核失败 |
|---|---|---|
| 默认 | 不审核，问人（和今天一样） | — |
| 辅助审批（`assisted`） | 自动执行 | 转人工确认，走原来的流程 |
| Autopilot（`autopilot`） | 自动执行 | 直接拒绝并返回原因，调用方的 agent 自己处理，不等人 |

**和 [独立 opsctl 审批](2026-08-17-standalone-opsctl-approval.md) 的关系：** 那份规范规定"没有任何免审批开关、既有权限判定语义不得放宽"。本方案经维护者同意作为例外，前提是：

1. 模式只能由人在桌面端设置（资产 / 分组的权限设置），不提供命令行参数或环境变量。
2. 禁止规则、放行规则、授权记录的判断顺序和结果不变，新模式只处理"原本要问人"的那部分。
3. 审核未通过或审核失败时不会多执行任何命令：辅助审批转人工确认，Autopilot 拒绝。
4. 不开启时和今天完全一样。

## 模型审核

- **输入**：资产类型名 + 替换掉密码的整条命令；AI 对话里另附用户本轮的要求。**不区分资产类型**，所有类型同一组题目、同一个通过标准。
- **题目**（全部是是/否题，任意一题"是"的概率 ≥ 阈值即不通过）：
  - `destructive`：删除、覆盖或不可恢复地修改数据、文件、数据库、用户、权限、凭据、密钥；
  - `disruptive`：停止 / 重启 / 杀掉 / 禁用 / 改配置正在运行的服务、容器、进程、主机，或改防火墙 / 网络；
  - `remote_code`：从网上下载代码或脚本并执行；
  - `beyond_request`（只在拿得到用户要求时问）：做了用户没要求的事。
- **审核所有操作**：读和写都可以通过，只要上面的题都不命中。
- **结果**：通过 / 未通过 / 失败。失败原因：未配置 API key、API key 无效、超时、命令过长（替换密码后超过 4,000 字符）、服务不可用。
- **缓存**：按"模型版本 + 题目版本 + 资产类型 + 替换密码后的命令 + 用户要求"的哈希存进 `command_reviews` 表，只存哈希和结果，7 天过期；审核失败不缓存。桌面端和 opsctl 共用同一个数据库，互相命中。
- **默认设置**：模型 `jev-1.13.0`（写死具体版本，不接受 `jev-latest` / `jev-preview`），超时 5 秒，阈值 0.2。

## 实现

| 部分 | 位置 |
|---|---|
| Jev HTTP 客户端（429 / 529 退避重试） | `internal/pkg/typesafe/` |
| 审核服务：替换密码、题目、判断、缓存、测试连接 | `internal/service/command_review_svc/` |
| 审核缓存表 | `internal/model/entity/command_review_entity/`、`internal/repository/command_review_repo/` |
| 迁移：`command_reviews` 表、`assets` / `groups.permission_mode`、`audit_logs.review` | `migrations/202609290001_command_review.go` |
| 权限模式取值与校验 | `internal/model/entity/policy/permission_mode.go`；资产和分组的 `Validate` 调用它 |
| 接入权限检查 | `internal/ai/permission/review.go` 的 `applyReview`，由 `CheckPermission`（`permission.go`）在规则判断之后调用 |
| 决策来源、审核结果类型、ctx 传值 | `internal/ai/aictx/decision.go`（`assisted_allow` / `autopilot_allow` / `autopilot_deny`）、`internal/ai/aictx/review.go` |
| 注册与设置 | `internal/bootstrap/command_review.go`（`Init` 里注册，桌面端和 opsctl 共用）、`AppConfig.CommandReview*`、`internal/app/system/command_review.go`（设置读写与测试连接） |

- **权限模式怎么生效**：AI 对话临时开启的 Autopilot（`aictx.WithAutopilot`，来自 `runner.AIContext.Autopilot`）优先；其次是资产自己的 `permission_mode`；为空时沿分组链向上找第一个设置了的；都没有就是默认。资产读取失败时按默认处理。
- **审核结果怎么传到人面前**：`CheckResult.Review` →
  - AI 对话：`CheckForAsset` 把它交给确认流程，放进 `ApprovalItem.Review`；
  - opsctl：放进 `approval.ApprovalRequest.Review` / `BatchItem.Review`，终端提示和桌面端弹窗都显示；
  - 人确认后的结果里保留审核结果，审计写进 `audit_logs.review`。
- **Autopilot 的拒绝**：返回"拒绝"和原因，各入口按现有方式输出（opsctl 为 `command denied by policy: <原因>`）。原因区分未通过（"不要原样重试"）和失败（"可以稍后重试"）。
- **审核服务没有注册时**（未经 `bootstrap.Init` 的进程，如单元测试）不审核。

## 待做

- 前端：资产 / 分组的权限模式选择（首次开启时确认）、对话输入框的 Autopilot 开关、审批弹窗显示审核结果、设置页、审计页的新来源标签和筛选。桌面端 `UpdateAsset` 保存前端传来的整个资产对象，前端必须带回 `permissionMode`，否则会被清空。
- 批量执行时并行审核（现在逐条审核）。
- 公开测试样本与阈值评估。
- 文档：`docs/ARCHITECTURE.md` 的权限流程、`plugin/opsctl/skills/opsctl/SKILL.md` 里的拒绝原因说明。
