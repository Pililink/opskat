import { useState, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Key, Loader2, Send, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useQueryStore } from "@/stores/queryStore";
import { ExecuteRedis } from "../../../wailsjs/go/main/App";

interface RedisKeyDetailProps {
  tabId: string;
}

interface RedisResult {
  type: string;
  value: unknown;
}

const TYPE_COLORS: Record<string, string> = {
  string: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
  hash: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
  list: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400",
  set: "bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-400",
  zset: "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400",
};

function formatResult(parsed: RedisResult): string {
  if (parsed.type === "nil") return "(nil)";
  if (parsed.type === "string" || parsed.type === "integer") {
    return String(parsed.value);
  }
  if (parsed.type === "list" && Array.isArray(parsed.value)) {
    return (parsed.value as unknown[])
      .map((v, i) => `${i + 1}) ${JSON.stringify(v)}`)
      .join("\n");
  }
  if (parsed.type === "hash" && typeof parsed.value === "object" && parsed.value !== null) {
    return Object.entries(parsed.value as Record<string, unknown>)
      .map(([k, v]) => `${k} => ${JSON.stringify(v)}`)
      .join("\n");
  }
  return JSON.stringify(parsed.value, null, 2);
}

export function RedisKeyDetail({ tabId }: RedisKeyDetailProps) {
  const { t } = useTranslation();
  const { redisStates, openTabs } = useQueryStore();
  const state = redisStates[tabId];
  const tab = openTabs.find((tb) => tb.id === tabId);

  const [command, setCommand] = useState("");
  const [executing, setExecuting] = useState(false);
  const [cmdResult, setCmdResult] = useState<string | null>(null);
  const [cmdError, setCmdError] = useState<string | null>(null);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIdx, setHistoryIdx] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);

  const executeCommand = useCallback(async () => {
    if (!command.trim() || !tab) return;

    setExecuting(true);
    setCmdResult(null);
    setCmdError(null);

    // Add to history
    setHistory((prev) => {
      const next = [command, ...prev.filter((c) => c !== command)].slice(0, 20);
      return next;
    });
    setHistoryIdx(-1);

    try {
      const result = await ExecuteRedis(tab.assetId, command.trim());
      const parsed: RedisResult = JSON.parse(result);
      setCmdResult(formatResult(parsed));
    } catch (err) {
      setCmdError(String(err));
    } finally {
      setExecuting(false);
    }
  }, [command, tab]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" && !executing) {
        e.preventDefault();
        executeCommand();
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        if (history.length === 0) return;
        const nextIdx = Math.min(historyIdx + 1, history.length - 1);
        setHistoryIdx(nextIdx);
        setCommand(history[nextIdx]);
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        if (historyIdx <= 0) {
          setHistoryIdx(-1);
          setCommand("");
          return;
        }
        const nextIdx = historyIdx - 1;
        setHistoryIdx(nextIdx);
        setCommand(history[nextIdx]);
      }
    },
    [executing, executeCommand, history, historyIdx]
  );

  if (!state) return null;

  // No key selected
  if (!state.selectedKey) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <div className="text-center">
          <Key className="mx-auto mb-2 size-8 opacity-40" />
          <p className="text-sm">{t("query.noKeySelected")}</p>
        </div>
      </div>
    );
  }

  // Key selected but info loading
  if (!state.keyInfo) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const { type, ttl, value } = state.keyInfo;

  const ttlDisplay =
    ttl === -1
      ? t("query.ttlPersist")
      : t("query.ttlSeconds", { seconds: ttl });

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <Key className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate font-mono text-sm font-medium">
          {state.selectedKey}
        </span>
        <span
          className={`inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium ${TYPE_COLORS[type] || "bg-muted text-muted-foreground"}`}
        >
          {type}
        </span>
        <span className="ml-auto text-xs text-muted-foreground">
          {t("query.ttl")}: {ttlDisplay}
        </span>
      </div>

      {/* Value display */}
      <ScrollArea className="flex-1">
        <div className="p-3">{renderValue(type, value, t)}</div>
      </ScrollArea>

      {/* Command input */}
      <div className="border-t">
        <div className="flex items-center gap-1 px-2 py-1.5">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
          <Input
            ref={inputRef}
            className="h-7 flex-1 font-mono text-xs"
            placeholder={t("query.redisPlaceholder")}
            value={command}
            onChange={(e) => {
              setCommand(e.target.value);
              setHistoryIdx(-1);
            }}
            onKeyDown={handleKeyDown}
            disabled={executing}
          />
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={executeCommand}
            disabled={executing || !command.trim()}
          >
            {executing ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Send className="size-3.5" />
            )}
          </Button>
        </div>

        {/* Command result */}
        {(cmdResult !== null || cmdError !== null) && (
          <div className="border-t px-3 py-2">
            {cmdError ? (
              <pre className="whitespace-pre-wrap font-mono text-xs text-destructive">
                {t("query.error")}: {cmdError}
              </pre>
            ) : (
              <pre className="whitespace-pre-wrap font-mono text-xs text-foreground">
                {cmdResult}
              </pre>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function renderValue(
  type: string,
  value: unknown,
  t: (key: string) => string
) {
  switch (type) {
    case "string":
      return (
        <pre className="whitespace-pre-wrap break-all rounded border bg-muted/50 p-3 font-mono text-xs">
          {String(value)}
        </pre>
      );

    case "hash": {
      const entries = Object.entries(
        (value as Record<string, string>) || {}
      );
      return (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b text-left">
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.field")}
              </th>
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.value")}
              </th>
            </tr>
          </thead>
          <tbody>
            {entries.map(([field, val]) => (
              <tr key={field} className="border-b last:border-0">
                <td className="px-2 py-1.5 font-mono text-foreground">
                  {field}
                </td>
                <td className="px-2 py-1.5 font-mono break-all text-foreground">
                  {val}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }

    case "list": {
      const items = (value as string[]) || [];
      return (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b text-left">
              <th className="w-16 px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.index")}
              </th>
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.value")}
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((item, idx) => (
              <tr key={idx} className="border-b last:border-0">
                <td className="px-2 py-1.5 font-mono text-muted-foreground">
                  {idx}
                </td>
                <td className="px-2 py-1.5 font-mono break-all text-foreground">
                  {item}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }

    case "set": {
      const members = (value as string[]) || [];
      return (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b text-left">
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.member")}
              </th>
            </tr>
          </thead>
          <tbody>
            {members.map((member, idx) => (
              <tr key={idx} className="border-b last:border-0">
                <td className="px-2 py-1.5 font-mono break-all text-foreground">
                  {member}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }

    case "zset": {
      // ZRANGE ... WITHSCORES returns [member, score, member, score, ...]
      const raw = (value as string[]) || [];
      const pairs: { member: string; score: string }[] = [];
      for (let i = 0; i < raw.length; i += 2) {
        pairs.push({ member: raw[i], score: raw[i + 1] || "0" });
      }
      return (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b text-left">
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.score")}
              </th>
              <th className="px-2 py-1.5 font-medium text-muted-foreground">
                {t("query.member")}
              </th>
            </tr>
          </thead>
          <tbody>
            {pairs.map((pair, idx) => (
              <tr key={idx} className="border-b last:border-0">
                <td className="w-24 px-2 py-1.5 font-mono text-muted-foreground">
                  {pair.score}
                </td>
                <td className="px-2 py-1.5 font-mono break-all text-foreground">
                  {pair.member}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }

    default:
      return (
        <pre className="whitespace-pre-wrap break-all rounded border bg-muted/50 p-3 font-mono text-xs">
          {JSON.stringify(value, null, 2)}
        </pre>
      );
  }
}
