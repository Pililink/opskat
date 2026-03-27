import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldAlert, Terminal, Database, Server, Globe, FolderOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { RespondAIApproval } from "../../../wailsjs/go/app/App";
import { ai } from "../../../wailsjs/go/models";
import type { ContentBlock } from "@/stores/aiStore";

interface ApprovalBlockProps {
  block: ContentBlock;
}

export function ApprovalBlock({ block }: ApprovalBlockProps) {
  const { t } = useTranslation();
  const isPending = block.status === "pending_confirm";
  const isDenied = block.status === "error";
  const items = block.approvalItems || [];
  const kind = block.approvalKind || "single";

  const [editedCommands, setEditedCommands] = useState<Record<number, string>>(() => {
    const map: Record<number, string> = {};
    items.forEach((item, i) => {
      map[i] = item.command;
    });
    return map;
  });

  const [rememberMode, setRememberMode] = useState(false);

  const respond = (decision: string) => {
    if (!block.confirmId) return;

    const resp = new ai.ApprovalResponse();
    resp.decision = decision;

    if ((kind === "grant" || (kind === "single" && decision === "allowAll")) && decision !== "deny") {
      resp.edited_items = items.map((item, i) => {
        const edited = new ai.ApprovalItem();
        edited.type = item.type;
        edited.asset_id = item.asset_id;
        edited.asset_name = item.asset_name;
        edited.group_id = item.group_id || 0;
        edited.group_name = item.group_name || "";
        edited.command = editedCommands[i] || item.command;
        edited.detail = item.detail || "";
        return edited;
      });
    }

    RespondAIApproval(block.confirmId, resp);
  };

  return (
    <div className="my-2 rounded-xl border border-amber-500/25 bg-amber-950/60 p-4 space-y-3 text-xs overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <ShieldAlert className="h-4 w-4 shrink-0 text-amber-500" />
          <span className="font-semibold text-sm text-amber-500">
            {kind === "grant"
              ? t("ai.approvalGrantTitle")
              : kind === "batch"
                ? t("ai.approvalBatchTitle", { count: items.length })
                : t("ai.approvalSingleTitle")}
          </span>
          {block.agentRole && (
            <span className="text-[10px] text-muted-foreground bg-muted rounded px-1 py-0.5">
              {block.agentRole}
            </span>
          )}
        </div>
        <div>
          {isPending && (
            <span className="rounded-full bg-amber-500/20 px-2 py-0.5 text-[10px] font-semibold text-amber-500">
              {t("ai.approvalPending", "等待确认")}
            </span>
          )}
          {!isPending && !isDenied && (
            <span className="rounded-full bg-green-500/20 px-2 py-0.5 text-[10px] font-semibold text-green-500">
              {t("ai.approvalApproved")}
            </span>
          )}
          {isDenied && (
            <span className="rounded-full bg-red-500/20 px-2 py-0.5 text-[10px] font-semibold text-red-500">
              {t("ai.approvalDenied", "已拒绝")}
            </span>
          )}
        </div>
      </div>

      {/* Description (grant only) */}
      {block.approvalDescription && (
        <div className="text-xs text-amber-200/70">{block.approvalDescription}</div>
      )}

      {/* Items */}
      <div className="space-y-2">
        {items.map((item, i) => (
          <div key={i} className="rounded-lg bg-amber-950/80 p-3 space-y-2">
            <div className="flex items-center gap-2">
              {kind === "grant" ? (
                <ScopeBadge item={item} />
              ) : (
                <>
                  <TypeBadge type={item.type} />
                  {item.asset_name && (
                    <span className="text-amber-200/70">{item.asset_name}</span>
                  )}
                </>
              )}
            </div>
            {kind === "grant" && isPending ? (
              <Textarea
                value={editedCommands[i] || ""}
                onChange={(e) =>
                  setEditedCommands((prev) => ({ ...prev, [i]: e.target.value }))
                }
                className="font-mono text-[11px] min-h-[32px] resize-y bg-black/30 border-amber-500/20"
                rows={Math.max(1, (editedCommands[i] || "").split("\n").length)}
              />
            ) : (
              <div className="rounded-md bg-black/30 p-2">
                <code className="block font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
                  {item.command}
                </code>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Remember mode pattern editor (single only) */}
      {kind === "single" && isPending && rememberMode && (
        <div className="space-y-1 pt-0.5">
          <div className="text-[10px] text-muted-foreground">{t("opsctlApproval.patternLabel")}</div>
          {items.map((_item, i) => (
            <Input
              key={i}
              value={editedCommands[i] || ""}
              onChange={(e) =>
                setEditedCommands((prev) => ({ ...prev, [i]: e.target.value }))
              }
              className="font-mono text-[11px] h-7 bg-black/30 border-amber-500/20"
              placeholder={t("opsctlApproval.patternPlaceholder")}
            />
          ))}
          <div className="text-[10px] text-muted-foreground/70">{t("opsctlApproval.patternHint")}</div>
        </div>
      )}

      {/* Action buttons */}
      {isPending && (
        <div className="flex justify-end gap-2 pt-1">
          {kind === "batch" ? (
            <>
              <Button size="sm" variant="outline" className="h-8 px-3 text-xs" onClick={() => respond("deny")}>
                {t("ai.approvalDenyAll")}
              </Button>
              <Button size="sm" className="h-8 px-3 text-xs bg-amber-500 hover:bg-amber-600 text-amber-950" onClick={() => respond("allow")}>
                {t("ai.approvalAllowAll")}
              </Button>
            </>
          ) : kind === "grant" ? (
            <>
              <Button size="sm" variant="outline" className="h-8 px-3 text-xs" onClick={() => respond("deny")}>
                {t("ai.approvalDeny")}
              </Button>
              <Button size="sm" className="h-8 px-3 text-xs bg-amber-500 hover:bg-amber-600 text-amber-950" onClick={() => respond("allow")}>
                {t("ai.approvalApprove")}
              </Button>
            </>
          ) : (
            <>
              <Button size="sm" variant="outline" className="h-8 px-3 text-xs" onClick={() => respond("deny")}>
                {t("ai.approvalDeny")}
              </Button>
              {rememberMode ? (
                <Button size="sm" variant="secondary" className="h-8 px-3 text-xs" onClick={() => respond("allowAll")}>
                  {t("ai.approvalRememberAndAllow")}
                </Button>
              ) : (
                <Button size="sm" variant="secondary" className="h-8 px-3 text-xs" onClick={() => {
                  setRememberMode(true);
                }}>
                  {t("opsctlApproval.remember")}
                </Button>
              )}
              <Button size="sm" className="h-8 px-3 text-xs bg-amber-500 hover:bg-amber-600 text-amber-950" onClick={() => respond("allow")}>
                {rememberMode ? t("ai.approvalOnlyOnce") : t("ai.approvalAllow")}
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function TypeBadge({ type }: { type: string }) {
  const icons: Record<string, typeof Terminal> = {
    exec: Terminal,
    sql: Database,
    redis: Server,
    grant: Globe,
  };
  const Icon = icons[type] || Terminal;
  return (
    <span className="inline-flex items-center gap-0.5 rounded border border-amber-500/30 px-1 py-0.5 text-[10px] font-medium text-amber-200/70 bg-black/20">
      <Icon className="h-3 w-3" />
      {type.toUpperCase()}
    </span>
  );
}

function ScopeBadge({ item }: { item: { asset_id: number; asset_name: string; group_id?: number; group_name?: string } }) {
  const { t } = useTranslation();
  const cls = "inline-flex items-center gap-1 rounded border border-amber-500/30 px-1.5 py-0.5 text-[10px] font-medium text-amber-200/70 bg-black/20";
  if (item.asset_id > 0) {
    return <span className={cls}><Server className="h-3 w-3" />{item.asset_name}</span>;
  }
  if (item.group_id && item.group_id > 0) {
    return <span className={cls}><FolderOpen className="h-3 w-3" />{item.group_name}</span>;
  }
  return <span className={cls}><Globe className="h-3 w-3" />{t("opsctlApproval.scopeAll")}</span>;
}
