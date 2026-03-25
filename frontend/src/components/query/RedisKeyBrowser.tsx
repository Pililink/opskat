import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { Database, RefreshCw, Loader2, Search, Key, AlertCircle, Copy, Trash2 } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useQueryStore } from "@/stores/queryStore";
import { useTabStore, type QueryTabMeta } from "@/stores/tabStore";
import { ExecuteRedisArgs } from "../../../wailsjs/go/main/App";

interface RedisKeyBrowserProps {
  tabId: string;
}

const KEY_ROW_HEIGHT = 28;

export function RedisKeyBrowser({ tabId }: RedisKeyBrowserProps) {
  const { t } = useTranslation();
  const { redisStates, scanKeys, selectRedisDb, selectKey, setKeyFilter, loadDbKeyCounts, removeKey } = useQueryStore();
  const state = redisStates[tabId];
  const tab = useTabStore((s) => s.tabs.find((tb) => tb.id === tabId));
  const tabMeta = tab?.meta as QueryTabMeta | undefined;
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Context menu state
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; key: string } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const virtualizer = useVirtualizer({
    count: state?.keys.length ?? 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => KEY_ROW_HEIGHT,
    overscan: 20,
  });

  useEffect(() => {
    scanKeys(tabId, true);
    loadDbKeyCounts(tabId);
  }, [tabId, scanKeys, loadDbKeyCounts]);

  // Close context menu on outside click / escape
  useEffect(() => {
    if (!ctxMenu) return;
    const close = () => setCtxMenu(null);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    const onPointer = (e: PointerEvent) => {
      if (ctxMenuRef.current?.contains(e.target as Node)) return;
      close();
    };
    const timer = setTimeout(() => {
      document.addEventListener("pointerdown", onPointer, true);
    }, 50);
    document.addEventListener("keydown", onKey);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("pointerdown", onPointer, true);
      document.removeEventListener("keydown", onKey);
    };
  }, [ctxMenu]);

  const handleDbChange = useCallback(
    (value: string) => {
      selectRedisDb(tabId, Number(value));
    },
    [tabId, selectRedisDb]
  );

  const handleFilterChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const pattern = e.target.value || "*";
      setKeyFilter(tabId, pattern);

      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
      debounceRef.current = setTimeout(() => {
        scanKeys(tabId, true);
      }, 300);
    },
    [tabId, setKeyFilter, scanKeys]
  );

  const handleRefresh = useCallback(() => {
    scanKeys(tabId, true);
    loadDbKeyCounts(tabId);
  }, [tabId, scanKeys, loadDbKeyCounts]);

  const handleLoadMore = useCallback(() => {
    scanKeys(tabId, false);
  }, [tabId, scanKeys]);

  const handleSelectKey = useCallback(
    (key: string) => {
      selectKey(tabId, key);
    },
    [tabId, selectKey]
  );

  const handleCopyKeyName = useCallback(() => {
    if (!ctxMenu) return;
    navigator.clipboard.writeText(ctxMenu.key);
    toast.success(t("query.copied"));
    setCtxMenu(null);
  }, [ctxMenu, t]);

  const handleDeleteFromCtx = useCallback(() => {
    if (!ctxMenu) return;
    setDeleteTarget(ctxMenu.key);
    setCtxMenu(null);
  }, [ctxMenu]);

  const confirmDelete = useCallback(async () => {
    if (!deleteTarget || !tabMeta || !state) return;
    try {
      await ExecuteRedisArgs(tabMeta.assetId, ["DEL", deleteTarget], state.currentDb);
      removeKey(tabId, deleteTarget);
      loadDbKeyCounts(tabId);
    } catch {
      /* ignore */
    }
    setDeleteTarget(null);
  }, [deleteTarget, tabMeta, state, tabId, removeKey, loadDbKeyCounts]);

  if (!state) return null;

  const dbOptions = Array.from({ length: 16 }, (_, i) => i);

  return (
    <div className="flex h-full flex-col">
      {/* DB selector + refresh */}
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        <Database className="size-3.5 shrink-0 text-muted-foreground" />
        <Select value={String(state.currentDb)} onValueChange={handleDbChange}>
          <SelectTrigger size="sm" className="h-7 flex-1 text-xs">
            <SelectValue placeholder={t("query.selectDb")} />
          </SelectTrigger>
          <SelectContent>
            {dbOptions.map((db) => {
              const count = state.dbKeyCounts[db];
              return (
                <SelectItem key={db} value={String(db)}>
                  <span className="flex items-center gap-1.5">
                    <span>db{db}</span>
                    {count !== undefined && count > 0 && <span className="text-muted-foreground">({count})</span>}
                  </span>
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        <Button variant="ghost" size="icon-xs" onClick={handleRefresh} disabled={state.loadingKeys}>
          <RefreshCw className={`size-3.5 ${state.loadingKeys ? "animate-spin" : ""}`} />
        </Button>
      </div>

      {/* Filter input */}
      <div className="border-b px-2 py-1.5">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 size-3 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-7 pl-7 text-xs"
            placeholder={t("query.filterKeys")}
            value={state.keyFilter === "*" ? "" : state.keyFilter}
            onChange={handleFilterChange}
          />
        </div>
      </div>

      {/* Key count */}
      <div className="border-b px-2 py-1 text-xs text-muted-foreground">
        {t("query.keyCount", { count: state.keys.length })}
      </div>

      {/* Error message */}
      {state.error && (
        <div className="flex items-start gap-2 border-b border-destructive/20 bg-destructive/10 px-2 py-2 text-xs text-destructive">
          <AlertCircle className="size-3.5 shrink-0 mt-0.5" />
          <span className="break-all">{state.error}</span>
        </div>
      )}

      {/* Virtualized key list */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const key = state.keys[virtualRow.index];
            return (
              <button
                key={key}
                className={`absolute left-0 flex w-full items-center gap-1.5 px-2 text-left text-xs font-mono hover:bg-accent ${
                  state.selectedKey === key ? "bg-accent text-accent-foreground" : ""
                }`}
                style={{
                  top: virtualRow.start,
                  height: virtualRow.size,
                }}
                onClick={() => handleSelectKey(key)}
                onContextMenu={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setCtxMenu({ x: e.clientX, y: e.clientY, key });
                }}
              >
                <Key className="size-3 shrink-0 text-muted-foreground" />
                <span className="truncate">{key}</span>
              </button>
            );
          })}
        </div>

        {state.loadingKeys && (
          <div className="flex items-center justify-center py-3">
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          </div>
        )}
      </div>

      {/* Load more */}
      {state.hasMore && !state.loadingKeys && (
        <div className="border-t px-2 py-1.5">
          <Button variant="ghost" size="sm" className="h-7 w-full text-xs" onClick={handleLoadMore}>
            {t("query.loadMore")}
          </Button>
        </div>
      )}

      {/* Key context menu */}
      {ctxMenu &&
        createPortal(
          <div
            ref={ctxMenuRef}
            className="z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md animate-in fade-in-0 zoom-in-95"
            style={{ position: "fixed", top: ctxMenu.y + 2, left: ctxMenu.x + 2 }}
          >
            <div
              role="menuitem"
              className="relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none hover:bg-accent hover:text-accent-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0"
              onClick={handleCopyKeyName}
            >
              <Copy className="size-3.5" />
              {t("query.copyKeyName")}
            </div>
            <div
              role="menuitem"
              className="relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none hover:bg-accent hover:text-accent-foreground text-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0"
              onClick={handleDeleteFromCtx}
            >
              <Trash2 className="size-3.5" />
              {t("query.deleteKey")}
            </div>
          </div>,
          document.body
        )}

      {/* Delete key confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
        title={t("query.deleteKey")}
        description={t("query.deleteKeyConfirmDesc", { name: deleteTarget ?? "" })}
        cancelText={t("action.cancel")}
        confirmText={t("action.delete")}
        onConfirm={confirmDelete}
      />
    </div>
  );
}
