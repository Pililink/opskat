import { useState, useRef, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { Loader2, Copy } from "lucide-react";
import { toast } from "sonner";

export interface CellEdit {
  rowIdx: number;
  col: string;
  value: unknown; // new value
}

interface QueryResultTableProps {
  columns: string[];
  rows: Record<string, unknown>[];
  loading?: boolean;
  error?: string;
  editable?: boolean;
  edits?: Map<string, unknown>; // key: "rowIdx:col"
  onCellEdit?: (edit: CellEdit) => void;
  showRowNumber?: boolean;
  rowNumberOffset?: number;
}

function cellKey(rowIdx: number, col: string) {
  return `${rowIdx}:${col}`;
}

export function QueryResultTable({
  columns,
  rows,
  loading,
  error,
  editable,
  edits,
  onCellEdit,
  showRowNumber,
  rowNumberOffset = 0,
}: QueryResultTableProps) {
  const { t } = useTranslation();

  const [editingCell, setEditingCell] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Context menu state
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; value: unknown } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement>(null);

  // Focus input when editing starts
  useEffect(() => {
    if (editingCell && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [editingCell]);

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

  const commitEdit = useCallback(
    (rowIdx: number, col: string, newValue: string) => {
      const original = rows[rowIdx]?.[col];
      const originalStr = original == null ? "" : String(original);
      if (newValue !== originalStr) {
        onCellEdit?.({
          rowIdx,
          col,
          value: newValue === "" && original == null ? null : newValue,
        });
      }
      setEditingCell(null);
    },
    [rows, onCellEdit]
  );

  const handleCopyCell = useCallback(() => {
    if (!ctxMenu) return;
    const text = ctxMenu.value == null ? "" : String(ctxMenu.value);
    navigator.clipboard.writeText(text);
    toast.success(t("query.copied"));
    setCtxMenu(null);
  }, [ctxMenu, t]);

  const handleCellContextMenu = useCallback((e: React.MouseEvent, value: unknown) => {
    e.preventDefault();
    e.stopPropagation();
    setCtxMenu({ x: e.clientX, y: e.clientY, value });
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return <div className="px-3 py-4 text-xs text-destructive whitespace-pre-wrap font-mono">{error}</div>;
  }

  if (columns.length === 0) {
    return (
      <div className="flex items-center justify-center py-8 text-xs text-muted-foreground">{t("query.noResult")}</div>
    );
  }

  return (
    <div className="overflow-auto flex-1 min-h-0">
      <table className="w-full border-collapse text-xs font-mono">
        <thead className="sticky top-0 z-10 bg-muted">
          <tr>
            {showRowNumber && (
              <th className="border border-border px-2 py-1.5 text-center font-semibold text-muted-foreground whitespace-nowrap w-[50px]">
                #
              </th>
            )}
            {columns.map((col) => (
              <th
                key={col}
                className="border border-border px-2 py-1.5 text-left font-semibold text-muted-foreground whitespace-nowrap"
              >
                {col}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, idx) => (
            <tr key={idx} className={idx % 2 === 0 ? "bg-background" : "bg-muted/40"}>
              {showRowNumber && (
                <td className="border border-border px-2 py-1 text-center text-muted-foreground whitespace-nowrap w-[50px]">
                  {rowNumberOffset + idx + 1}
                </td>
              )}
              {columns.map((col) => {
                const ck = cellKey(idx, col);
                const isEdited = edits?.has(ck);
                const displayValue = isEdited ? edits!.get(ck) : row[col];
                const isEditing = editingCell === ck;

                return (
                  <td
                    key={col}
                    className={`border border-border px-2 py-1 whitespace-nowrap max-w-[400px] ${
                      isEdited ? "bg-yellow-100 dark:bg-yellow-900/30" : ""
                    }`}
                    title={displayValue == null ? "NULL" : String(displayValue)}
                    onDoubleClick={() => {
                      if (!editable) return;
                      setEditingCell(ck);
                    }}
                    onContextMenu={(e) => handleCellContextMenu(e, displayValue)}
                  >
                    {isEditing ? (
                      <input
                        ref={inputRef}
                        className="w-full bg-transparent outline-none border-none p-0 m-0 text-xs font-mono"
                        defaultValue={displayValue == null ? "" : String(displayValue)}
                        onBlur={(e) => commitEdit(idx, col, e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            commitEdit(idx, col, (e.target as HTMLInputElement).value);
                          }
                          if (e.key === "Escape") {
                            setEditingCell(null);
                          }
                        }}
                      />
                    ) : displayValue == null ? (
                      <span className="text-muted-foreground italic">NULL</span>
                    ) : (
                      <span className="truncate block max-w-[400px]">{String(displayValue)}</span>
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>

      {/* Cell context menu */}
      {ctxMenu &&
        createPortal(
          <div
            ref={ctxMenuRef}
            className="z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md animate-in fade-in-0 zoom-in-95"
            style={{ position: "fixed", top: ctxMenu.y + 2, left: ctxMenu.x + 2 }}
          >
            <div
              role="menuitem"
              className="relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none hover:bg-accent hover:text-accent-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground"
              onClick={handleCopyCell}
            >
              <Copy className="h-3.5 w-3.5" />
              {t("query.copyValue")}
            </div>
          </div>,
          document.body
        )}
    </div>
  );
}
