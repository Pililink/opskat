import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
import { RespondOpsctlApproval, RespondOpsctlApprovalSession } from "../../../wailsjs/go/main/App";
import { ShieldAlert } from "lucide-react";

interface ApprovalEvent {
  confirm_id: string;
  type: string;
  asset_id: number;
  asset_name: string;
  command: string;
  detail: string;
  session_id?: string;
}

export function OpsctlApprovalDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [event, setEvent] = useState<ApprovalEvent | null>(null);
  const [commandPattern, setCommandPattern] = useState("");

  const handleEvent = useCallback((data: ApprovalEvent) => {
    setEvent(data);
    setCommandPattern(data.command || "");
    setOpen(true);
  }, []);

  useEffect(() => {
    EventsOn("opsctl:approval", handleEvent);
    return () => { EventsOff("opsctl:approval"); };
  }, [handleEvent]);

  const respond = useCallback((approved: boolean) => {
    if (event) {
      RespondOpsctlApproval(event.confirm_id, approved);
    }
    setOpen(false);
    setEvent(null);
  }, [event]);

  const respondRemember = useCallback(() => {
    if (event && event.session_id && commandPattern) {
      RespondOpsctlApprovalSession(
        event.confirm_id, true, event.session_id,
        event.asset_id, commandPattern
      );
    }
    setOpen(false);
    setEvent(null);
  }, [event, commandPattern]);

  const typeLabel = event ? t(`opsctlApproval.type${event.type.charAt(0).toUpperCase() + event.type.slice(1)}`) : "";

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) respond(false); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldAlert className="h-5 w-5 text-amber-500" />
            {t("opsctlApproval.title")}
          </DialogTitle>
          <DialogDescription>
            {t("opsctlApproval.description")}
          </DialogDescription>
        </DialogHeader>
        {event && (
          <div className="space-y-3">
            {event.session_id && (
              <div className="text-xs text-blue-500 font-medium">
                {t("opsctlApproval.sessionHint")}
              </div>
            )}
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium">{typeLabel}</span>
              {event.asset_name && (
                <span className="text-sm text-muted-foreground">
                  {event.asset_name}
                  {event.asset_id > 0 && ` (ID: ${event.asset_id})`}
                </span>
              )}
            </div>
            {event.command && (
              <div className="rounded-md bg-muted p-3">
                <code className="text-sm font-mono whitespace-pre-wrap break-all">{event.command}</code>
              </div>
            )}
            <div className="text-xs text-muted-foreground font-mono">{event.detail}</div>
            {event.session_id && event.command && (
              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("opsctlApproval.patternLabel")}
                </label>
                <Input
                  value={commandPattern}
                  onChange={(e) => setCommandPattern(e.target.value)}
                  placeholder={t("opsctlApproval.patternPlaceholder")}
                  className="font-mono text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  {t("opsctlApproval.patternHint")}
                </p>
              </div>
            )}
          </div>
        )}
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => respond(false)}>
            {t("opsctlApproval.deny")}
          </Button>
          <Button variant="secondary" onClick={() => respond(true)}>
            {t("opsctlApproval.allow")}
          </Button>
          {event?.session_id && event?.command && (
            <Button onClick={respondRemember} disabled={!commandPattern.trim()}>
              {t("opsctlApproval.remember")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
