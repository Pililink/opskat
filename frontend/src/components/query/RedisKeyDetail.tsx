import { useState, useCallback, useRef, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Key, Loader2, Send, ChevronRight, Trash2, Pencil, Check, X, Plus, RefreshCw, Copy } from "lucide-react";
import { toast } from "sonner";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useQueryStore, RedisKeyInfo } from "@/stores/queryStore";
import { useTabStore, type QueryTabMeta } from "@/stores/tabStore";
import { ExecuteRedis, ExecuteRedisArgs } from "../../../wailsjs/go/main/App";

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

const VALUE_ROW_HEIGHT = 30;

function formatResult(parsed: RedisResult): string {
  if (parsed.type === "nil") return "(nil)";
  if (parsed.type === "string" || parsed.type === "integer") {
    return String(parsed.value);
  }
  if (parsed.type === "list" && Array.isArray(parsed.value)) {
    return (parsed.value as unknown[]).map((v, i) => `${i + 1}) ${JSON.stringify(v)}`).join("\n");
  }
  if (parsed.type === "hash" && typeof parsed.value === "object" && parsed.value !== null) {
    return Object.entries(parsed.value as Record<string, unknown>)
      .map(([k, v]) => `${k} => ${JSON.stringify(v)}`)
      .join("\n");
  }
  return JSON.stringify(parsed.value, null, 2);
}

function getItemCount(info: RedisKeyInfo): number {
  switch (info.type) {
    case "hash":
      return ((info.value as [string, string][]) || []).length;
    case "list":
    case "set":
      return ((info.value as string[]) || []).length;
    case "zset":
      return ((info.value as [string, string][]) || []).length;
    default:
      return 0;
  }
}

// --- Inline edit row ---

function EditableCell({
  value,
  onSave,
  className,
}: {
  value: string;
  onSave: (val: string) => Promise<void>;
  className?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [editVal, setEditVal] = useState(value);
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const startEdit = () => {
    setEditVal(value);
    setEditing(true);
    setTimeout(() => inputRef.current?.focus(), 0);
  };

  const save = async () => {
    if (editVal === value) {
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      await onSave(editVal);
      setEditing(false);
    } catch {
      /* keep editing */
    }
    setSaving(false);
  };

  const cancel = () => {
    setEditing(false);
    setEditVal(value);
  };

  if (editing) {
    return (
      <div className={`flex items-center gap-0.5 ${className || ""}`}>
        <Input
          ref={inputRef}
          className="h-6 flex-1 font-mono text-xs"
          value={editVal}
          onChange={(e) => setEditVal(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") save();
            if (e.key === "Escape") cancel();
          }}
          disabled={saving}
        />
        <Button variant="ghost" size="icon-xs" onClick={save} disabled={saving}>
          <Check className="size-3 text-green-600" />
        </Button>
        <Button variant="ghost" size="icon-xs" onClick={cancel} disabled={saving}>
          <X className="size-3" />
        </Button>
      </div>
    );
  }

  return (
    <div
      className={`group/cell flex cursor-pointer items-center truncate ${className || ""}`}
      onDoubleClick={startEdit}
    >
      <span className="truncate">{value}</span>
      <button className="ml-auto hidden shrink-0 group-hover/cell:inline-flex" onClick={startEdit}>
        <Pencil className="size-3 text-muted-foreground hover:text-foreground" />
      </button>
    </div>
  );
}

// --- Add row form ---

function AddRowForm({
  type,
  onAdd,
  t,
}: {
  type: string;
  onAdd: (args: string[]) => Promise<void>;
  t: (key: string) => string;
}) {
  const [field, setField] = useState("");
  const [value, setValue] = useState("");
  const [score, setScore] = useState("0");
  const [adding, setAdding] = useState(false);
  const submit = async () => {
    setAdding(true);
    try {
      if (type === "hash") {
        await onAdd([field, value]);
        setField("");
        setValue("");
      } else if (type === "list") {
        await onAdd([value]);
        setValue("");
      } else if (type === "set") {
        await onAdd([value]);
        setValue("");
      } else if (type === "zset") {
        await onAdd([score, value]);
        setScore("0");
        setValue("");
      }
    } catch {
      /* ignore */
    }
    setAdding(false);
  };

  return (
    <div className="flex items-center gap-1 border-t px-2 py-1.5">
      {type === "hash" && (
        <>
          <Input
            className="h-6 w-1/3 shrink-0 font-mono text-xs"
            placeholder={t("query.newField")}
            value={field}
            onChange={(e) => setField(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
          <Input
            className="h-6 flex-1 font-mono text-xs"
            placeholder={t("query.newValue")}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
        </>
      )}
      {(type === "list" || type === "set") && (
        <Input
          className="h-6 flex-1 font-mono text-xs"
          placeholder={type === "set" ? t("query.newMember") : t("query.newValue")}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
      )}
      {type === "zset" && (
        <>
          <Input
            className="h-6 w-20 shrink-0 font-mono text-xs"
            placeholder={t("query.newScore")}
            value={score}
            onChange={(e) => setScore(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
          <Input
            className="h-6 flex-1 font-mono text-xs"
            placeholder={t("query.newMember")}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
        </>
      )}
      <Button variant="ghost" size="icon-xs" onClick={submit} disabled={adding}>
        {adding ? <Loader2 className="size-3 animate-spin" /> : <Plus className="size-3" />}
      </Button>
    </div>
  );
}

// --- Collection Table ---

function CollectionTable({
  info,
  tabId,
  t,
}: {
  info: RedisKeyInfo;
  tabId: string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  const { loadMoreValues, selectKey, redisStates } = useQueryStore();
  const state = redisStates[tabId];
  const tab = useTabStore((s) => s.tabs.find((tb) => tb.id === tabId));
  const tabMeta = tab?.meta as QueryTabMeta | undefined;
  const scrollRef = useRef<HTMLDivElement>(null);
  const itemCount = getItemCount(info);
  const selectedKey = state?.selectedKey;
  const db = state?.currentDb ?? 0;
  const [deleteTarget, setDeleteTarget] = useState<{ label: string; action: () => void } | null>(null);

  const virtualizer = useVirtualizer({
    count: itemCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => VALUE_ROW_HEIGHT,
    overscan: 20,
  });

  const totalLabel =
    info.total >= 0 ? t("query.loadedOfTotal", { loaded: itemCount, total: info.total }) : `${itemCount}`;

  const handleEditHash = async (field: string, newVal: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["HSET", selectedKey, field, newVal], db);
    selectKey(tabId, selectedKey);
  };

  const handleDeleteHash = async (field: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["HDEL", selectedKey, field], db);
    selectKey(tabId, selectedKey);
  };

  const handleEditList = async (index: number, newVal: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["LSET", selectedKey, String(index), newVal], db);
    selectKey(tabId, selectedKey);
  };

  const handleDeleteList = async (index: number) => {
    if (!tabMeta || !selectedKey) return;
    const sentinel = "__OPSCAT_DEL_" + Date.now() + "__";
    await ExecuteRedisArgs(tabMeta.assetId, ["LSET", selectedKey, String(index), sentinel], db);
    await ExecuteRedisArgs(tabMeta.assetId, ["LREM", selectedKey, "1", sentinel], db);
    selectKey(tabId, selectedKey);
  };

  const handleDeleteSet = async (member: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["SREM", selectedKey, member], db);
    selectKey(tabId, selectedKey);
  };

  const handleEditZsetScore = async (member: string, newScore: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["ZADD", selectedKey, newScore, member], db);
    selectKey(tabId, selectedKey);
  };

  const handleDeleteZset = async (member: string) => {
    if (!tabMeta || !selectedKey) return;
    await ExecuteRedisArgs(tabMeta.assetId, ["ZREM", selectedKey, member], db);
    selectKey(tabId, selectedKey);
  };

  const handleAdd = async (args: string[]) => {
    if (!tabMeta || !selectedKey) return;
    if (info.type === "hash" && args.length === 2) {
      await ExecuteRedisArgs(tabMeta.assetId, ["HSET", selectedKey, args[0], args[1]], db);
    } else if (info.type === "list" && args.length === 1) {
      await ExecuteRedisArgs(tabMeta.assetId, ["RPUSH", selectedKey, args[0]], db);
    } else if (info.type === "set" && args.length === 1) {
      await ExecuteRedisArgs(tabMeta.assetId, ["SADD", selectedKey, args[0]], db);
    } else if (info.type === "zset" && args.length === 2) {
      await ExecuteRedisArgs(tabMeta.assetId, ["ZADD", selectedKey, args[0], args[1]], db);
    }
    selectKey(tabId, selectedKey);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Table header */}
      <div className="flex items-center border-b text-xs">
        {info.type === "hash" && (
          <>
            <div className="w-1/3 shrink-0 px-2 py-1.5 font-medium text-muted-foreground">{t("query.field")}</div>
            <div className="flex-1 px-2 py-1.5 font-medium text-muted-foreground">{t("query.value")}</div>
          </>
        )}
        {info.type === "list" && (
          <>
            <div className="w-16 shrink-0 px-2 py-1.5 font-medium text-muted-foreground">{t("query.index")}</div>
            <div className="flex-1 px-2 py-1.5 font-medium text-muted-foreground">{t("query.value")}</div>
          </>
        )}
        {info.type === "set" && (
          <div className="flex-1 px-2 py-1.5 font-medium text-muted-foreground">{t("query.member")}</div>
        )}
        {info.type === "zset" && (
          <>
            <div className="w-24 shrink-0 px-2 py-1.5 font-medium text-muted-foreground">{t("query.score")}</div>
            <div className="flex-1 px-2 py-1.5 font-medium text-muted-foreground">{t("query.member")}</div>
          </>
        )}
        <div className="shrink-0 px-2 py-1.5 text-xs text-muted-foreground">{totalLabel}</div>
      </div>

      {/* Virtualized rows */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const idx = virtualRow.index;
            return (
              <div
                key={virtualRow.key}
                className="group/row absolute left-0 flex w-full items-center border-b text-xs font-mono last:border-0"
                style={{ top: virtualRow.start, height: virtualRow.size }}
              >
                {info.type === "hash" &&
                  (() => {
                    const entry = (info.value as [string, string][])[idx];
                    return (
                      <>
                        <div className="w-1/3 shrink-0 truncate px-2 text-foreground">{entry[0]}</div>
                        <EditableCell
                          value={entry[1]}
                          onSave={(v) => handleEditHash(entry[0], v)}
                          className="flex-1 px-2 text-foreground"
                        />
                      </>
                    );
                  })()}
                {info.type === "list" && (
                  <>
                    <div className="w-16 shrink-0 px-2 text-muted-foreground">{idx}</div>
                    <EditableCell
                      value={(info.value as string[])[idx]}
                      onSave={(v) => handleEditList(idx, v)}
                      className="flex-1 px-2 text-foreground"
                    />
                  </>
                )}
                {info.type === "set" && (
                  <div className="flex flex-1 items-center truncate px-2 text-foreground">
                    <span className="truncate">{(info.value as string[])[idx]}</span>
                  </div>
                )}
                {info.type === "zset" &&
                  (() => {
                    const pair = (info.value as [string, string][])[idx];
                    return (
                      <>
                        <EditableCell
                          value={pair[1]}
                          onSave={(v) => handleEditZsetScore(pair[0], v)}
                          className="w-24 shrink-0 px-2 text-muted-foreground"
                        />
                        <div className="flex-1 truncate px-2 text-foreground">{pair[0]}</div>
                      </>
                    );
                  })()}
                {/* Row delete button */}
                <button
                  className="mr-1 hidden shrink-0 group-hover/row:inline-flex"
                  onClick={() => {
                    let label = "";
                    let action = () => {};
                    if (info.type === "hash") {
                      const field = (info.value as [string, string][])[idx][0];
                      label = field;
                      action = () => handleDeleteHash(field);
                    } else if (info.type === "list") {
                      label = `index ${idx}`;
                      action = () => handleDeleteList(idx);
                    } else if (info.type === "set") {
                      const member = (info.value as string[])[idx];
                      label = member;
                      action = () => handleDeleteSet(member);
                    } else if (info.type === "zset") {
                      const member = (info.value as [string, string][])[idx][0];
                      label = member;
                      action = () => handleDeleteZset(member);
                    }
                    setDeleteTarget({ label, action });
                  }}
                >
                  <Trash2 className="size-3 text-muted-foreground hover:text-destructive" />
                </button>
              </div>
            );
          })}
        </div>
      </div>

      {/* Load more values */}
      {info.hasMoreValues && (
        <div className="border-t px-2 py-1.5">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 w-full text-xs"
            onClick={() => loadMoreValues(tabId)}
            disabled={info.loadingMore}
          >
            {info.loadingMore ? <Loader2 className="mr-1 size-3 animate-spin" /> : null}
            {t("query.loadMore")}
          </Button>
        </div>
      )}

      {/* Add row */}
      <AddRowForm type={info.type} onAdd={handleAdd} t={t} />

      {/* Delete element confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
        title={t("query.deleteElementTitle")}
        description={t("query.deleteElementDesc", { name: deleteTarget?.label ?? "" })}
        cancelText={t("action.cancel")}
        confirmText={t("action.delete")}
        onConfirm={() => {
          deleteTarget?.action();
          setDeleteTarget(null);
        }}
      />
    </div>
  );
}

// --- String editor ---

function StringEditor({ tabId, t }: { tabId: string; t: (key: string) => string }) {
  const { redisStates, selectKey } = useQueryStore();
  const state = redisStates[tabId];
  const tab = useTabStore((s) => s.tabs.find((tb) => tb.id === tabId));
  const tabMeta = tab?.meta as QueryTabMeta | undefined;
  const [editing, setEditing] = useState(false);
  const [editVal, setEditVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [jsonFormatted, setJsonFormatted] = useState(true);

  const originalVal = String(state?.keyInfo?.value ?? "");

  const isJson = useMemo(() => {
    try {
      const trimmed = originalVal.trim();
      if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return false;
      JSON.parse(trimmed);
      return true;
    } catch {
      return false;
    }
  }, [originalVal]);

  const displayValue = useMemo(() => {
    if (isJson && jsonFormatted) {
      try {
        return JSON.stringify(JSON.parse(originalVal), null, 2);
      } catch {
        return originalVal;
      }
    }
    return originalVal;
  }, [isJson, jsonFormatted, originalVal]);

  if (!state?.keyInfo || !state.selectedKey || !tabMeta) return null;

  const db = state.currentDb;

  const startEdit = () => {
    setEditVal(originalVal);
    setEditing(true);
  };

  const save = async () => {
    setSaving(true);
    try {
      await ExecuteRedisArgs(tabMeta.assetId, ["SET", state.selectedKey!, editVal], db);
      selectKey(tabId, state.selectedKey!);
      setEditing(false);
    } catch {
      /* keep editing */
    }
    setSaving(false);
  };

  const cancel = () => {
    setEditing(false);
  };

  if (editing) {
    return (
      <div className="flex flex-1 flex-col">
        <Textarea
          className="flex-1 resize-none font-mono text-xs"
          value={editVal}
          onChange={(e) => setEditVal(e.target.value)}
        />
        <div className="flex items-center justify-end gap-1 border-t px-2 py-1.5">
          <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={cancel} disabled={saving}>
            {t("query.cancelEdit")}
          </Button>
          <Button size="sm" className="h-7 text-xs" onClick={save} disabled={saving}>
            {saving && <Loader2 className="mr-1 size-3 animate-spin" />}
            {t("query.saveValue")}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <ScrollArea className="flex-1">
      <div className="p-3">
        {/* JSON format toggle */}
        {isJson && (
          <div className="mb-2 flex justify-end">
            <div className="inline-flex rounded-md border text-xs">
              <button
                className={`px-2 py-0.5 rounded-l-md ${jsonFormatted ? "bg-accent text-accent-foreground" : ""}`}
                onClick={() => setJsonFormatted(true)}
              >
                {t("query.formatJson")}
              </button>
              <button
                className={`px-2 py-0.5 rounded-r-md ${!jsonFormatted ? "bg-accent text-accent-foreground" : ""}`}
                onClick={() => setJsonFormatted(false)}
              >
                {t("query.rawText")}
              </button>
            </div>
          </div>
        )}
        <div className="group/str relative">
          <pre className="whitespace-pre-wrap break-all rounded border bg-muted/50 p-3 font-mono text-xs">
            {displayValue}
          </pre>
          <Button
            variant="ghost"
            size="icon-xs"
            className="absolute right-2 top-2 hidden group-hover/str:inline-flex"
            onClick={startEdit}
          >
            <Pencil className="size-3" />
          </Button>
        </div>
      </div>
    </ScrollArea>
  );
}

// --- Main component ---

export function RedisKeyDetail({ tabId }: RedisKeyDetailProps) {
  const { t } = useTranslation();
  const { redisStates, removeKey, loadDbKeyCounts, selectKey } = useQueryStore();
  const state = redisStates[tabId];
  const tab = useTabStore((s) => s.tabs.find((tb) => tb.id === tabId));
  const tabMeta = tab?.meta as QueryTabMeta | undefined;

  const [command, setCommand] = useState("");
  const [executing, setExecuting] = useState(false);
  const [cmdResult, setCmdResult] = useState<string | null>(null);
  const [cmdError, setCmdError] = useState<string | null>(null);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIdx, setHistoryIdx] = useState(-1);
  const [deleting, setDeleting] = useState(false);
  const [showDeleteKeyConfirm, setShowDeleteKeyConfirm] = useState(false);
  const [editingTtl, setEditingTtl] = useState(false);
  const [ttlInput, setTtlInput] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const ttlInputRef = useRef<HTMLInputElement>(null);

  const executeCommand = useCallback(async () => {
    if (!command.trim() || !tabMeta || !state) return;

    setExecuting(true);
    setCmdResult(null);
    setCmdError(null);

    setHistory((prev) => {
      const next = [command, ...prev.filter((c) => c !== command)].slice(0, 20);
      return next;
    });
    setHistoryIdx(-1);

    try {
      const result = await ExecuteRedis(tabMeta.assetId, command.trim(), state.currentDb);
      const parsed: RedisResult = JSON.parse(result);
      setCmdResult(formatResult(parsed));
    } catch (err) {
      setCmdError(String(err));
    } finally {
      setExecuting(false);
    }
  }, [command, tabMeta, state]);

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

  const handleDeleteKey = useCallback(async () => {
    if (!tabMeta || !state?.selectedKey) return;
    setDeleting(true);
    try {
      await ExecuteRedisArgs(tabMeta.assetId, ["DEL", state.selectedKey], state.currentDb);
      removeKey(tabId, state.selectedKey);
      loadDbKeyCounts(tabId);
    } catch {
      /* ignore */
    }
    setDeleting(false);
  }, [tabMeta, state, tabId, removeKey, loadDbKeyCounts]);

  const handleRefreshKey = useCallback(() => {
    if (state?.selectedKey) {
      selectKey(tabId, state.selectedKey);
    }
  }, [state?.selectedKey, tabId, selectKey]);

  const handleCopyKeyName = useCallback(() => {
    if (state?.selectedKey) {
      navigator.clipboard.writeText(state.selectedKey);
      toast.success(t("query.copied"));
    }
  }, [state?.selectedKey, t]);

  const startTtlEdit = useCallback(() => {
    if (!state?.keyInfo) return;
    setTtlInput(state.keyInfo.ttl > 0 ? String(state.keyInfo.ttl) : "");
    setEditingTtl(true);
    setTimeout(() => ttlInputRef.current?.focus(), 0);
  }, [state?.keyInfo]);

  const saveTtl = useCallback(async () => {
    if (!tabMeta || !state?.selectedKey) return;
    const seconds = parseInt(ttlInput, 10);
    if (isNaN(seconds) || seconds <= 0) return;
    try {
      await ExecuteRedisArgs(tabMeta.assetId, ["EXPIRE", state.selectedKey, String(seconds)], state.currentDb);
      selectKey(tabId, state.selectedKey);
    } catch {
      /* ignore */
    }
    setEditingTtl(false);
  }, [tabMeta, state, ttlInput, tabId, selectKey]);

  const persistKey = useCallback(async () => {
    if (!tabMeta || !state?.selectedKey) return;
    try {
      await ExecuteRedisArgs(tabMeta.assetId, ["PERSIST", state.selectedKey], state.currentDb);
      selectKey(tabId, state.selectedKey);
    } catch {
      /* ignore */
    }
    setEditingTtl(false);
  }, [tabMeta, state, tabId, selectKey]);

  if (!state) return null;

  const hasKey = !!state.selectedKey;
  const keyInfo = state.keyInfo;
  const type = keyInfo?.type;
  const ttl = keyInfo?.ttl ?? -1;
  const isCollection = type === "hash" || type === "list" || type === "set" || type === "zset";

  const ttlDisplay = ttl === -1 ? t("query.ttlPersist") : t("query.ttlSeconds", { seconds: ttl });

  return (
    <div className="flex h-full flex-col">
      {/* Main content area */}
      <div className="flex-1 min-h-0 flex flex-col">
        {!hasKey ? (
          <div className="flex flex-1 items-center justify-center text-muted-foreground">
            <div className="text-center">
              <Key className="mx-auto mb-2 size-8 opacity-40" />
              <p className="text-sm">{t("query.noKeySelected")}</p>
            </div>
          </div>
        ) : !keyInfo ? (
          <div className="flex flex-1 items-center justify-center">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <>
            {/* Header */}
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <Key className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate font-mono text-sm font-medium">{state.selectedKey}</span>
              <Button variant="ghost" size="icon-xs" onClick={handleCopyKeyName} title={t("query.copyKeyName")}>
                <Copy className="size-3 text-muted-foreground" />
              </Button>
              <span
                className={`inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium ${TYPE_COLORS[type!] || "bg-muted text-muted-foreground"}`}
              >
                {type}
              </span>
              {/* TTL - editable */}
              {editingTtl ? (
                <div className="flex items-center gap-1">
                  <Input
                    ref={ttlInputRef}
                    className="h-6 w-20 font-mono text-xs"
                    placeholder={t("query.ttlInput")}
                    value={ttlInput}
                    onChange={(e) => setTtlInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") saveTtl();
                      if (e.key === "Escape") setEditingTtl(false);
                    }}
                  />
                  <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={saveTtl}>
                    {t("query.setTtl")}
                  </Button>
                  {ttl > 0 && (
                    <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={persistKey}>
                      {t("query.persist")}
                    </Button>
                  )}
                  <Button variant="ghost" size="icon-xs" onClick={() => setEditingTtl(false)}>
                    <X className="size-3" />
                  </Button>
                </div>
              ) : (
                <span
                  className="text-xs text-muted-foreground cursor-pointer hover:text-foreground"
                  onClick={startTtlEdit}
                  title={t("query.setTtl")}
                >
                  {t("query.ttl")}: {ttlDisplay}
                </span>
              )}
              <div className="ml-auto flex items-center gap-0.5">
                <Button variant="ghost" size="icon-xs" onClick={handleRefreshKey} title={t("query.refreshKey")}>
                  <RefreshCw className="size-3.5 text-muted-foreground" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => setShowDeleteKeyConfirm(true)}
                  disabled={deleting}
                  title={t("query.deleteKey")}
                >
                  {deleting ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Trash2 className="size-3.5 text-muted-foreground hover:text-destructive" />
                  )}
                </Button>
              </div>
            </div>

            {/* Value display */}
            {isCollection ? (
              <div className="min-h-0 flex-1">
                <CollectionTable info={keyInfo} tabId={tabId} t={t} />
              </div>
            ) : (
              <StringEditor tabId={tabId} t={t} />
            )}
          </>
        )}
      </div>

      {/* Command input - always visible */}
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
          <Button variant="ghost" size="icon-xs" onClick={executeCommand} disabled={executing || !command.trim()}>
            {executing ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
          </Button>
        </div>

        {/* Command result - scrollable with clear */}
        {(cmdResult !== null || cmdError !== null) && (
          <div className="relative border-t max-h-[200px] overflow-auto px-3 py-2">
            <button
              className="absolute right-2 top-2"
              onClick={() => {
                setCmdResult(null);
                setCmdError(null);
              }}
              title={t("query.clearResult")}
            >
              <X className="size-3 text-muted-foreground hover:text-foreground" />
            </button>
            {cmdError ? (
              <pre className="whitespace-pre-wrap font-mono text-xs text-destructive pr-6">
                {t("query.error")}: {cmdError}
              </pre>
            ) : (
              <pre className="whitespace-pre-wrap font-mono text-xs text-foreground pr-6">{cmdResult}</pre>
            )}
          </div>
        )}
      </div>

      {/* Delete key confirm */}
      <ConfirmDialog
        open={showDeleteKeyConfirm}
        onOpenChange={setShowDeleteKeyConfirm}
        title={t("query.deleteKey")}
        description={t("query.deleteKeyConfirmDesc", { name: state.selectedKey ?? "" })}
        cancelText={t("action.cancel")}
        confirmText={t("action.delete")}
        onConfirm={handleDeleteKey}
      />
    </div>
  );
}
