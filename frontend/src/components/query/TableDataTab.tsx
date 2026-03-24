import { useEffect, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useQueryStore } from "@/stores/queryStore";
import { ExecuteSQL } from "../../../wailsjs/go/main/App";
import { QueryResultTable } from "./QueryResultTable";

interface TableDataTabProps {
  tabId: string;
  database: string;
  table: string;
}

const PAGE_SIZE = 100;

interface SQLResult {
  columns?: string[];
  rows?: Record<string, unknown>[];
  count?: number;
  affected_rows?: number;
}

export function TableDataTab({ tabId, database, table }: TableDataTabProps) {
  const { t } = useTranslation();
  const { openTabs } = useQueryStore();

  const [columns, setColumns] = useState<string[]>([]);
  const [rows, setRows] = useState<Record<string, unknown>[]>([]);
  const [totalRows, setTotalRows] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const queryTab = openTabs.find((t) => t.id === tabId);
  const driver = queryTab?.driver;
  const assetId = queryTab?.assetId ?? 0;

  const fetchData = useCallback(
    async (pageNum: number) => {
      if (!assetId) return;
      setLoading(true);
      setError(null);

      const offset = pageNum * PAGE_SIZE;
      let sql: string;
      let db: string;

      if (driver === "postgresql") {
        sql = `SELECT * FROM "${table}" LIMIT ${PAGE_SIZE} OFFSET ${offset}`;
        db = database;
      } else {
        sql = `SELECT * FROM \`${database}\`.\`${table}\` LIMIT ${PAGE_SIZE} OFFSET ${offset}`;
        db = database;
      }

      try {
        const result = await ExecuteSQL(assetId, sql, db);
        const parsed: SQLResult = JSON.parse(result);
        setColumns(parsed.columns || []);
        setRows(parsed.rows || []);
        setTotalRows(parsed.count ?? (parsed.rows || []).length);
      } catch (e) {
        setError(String(e));
        setColumns([]);
        setRows([]);
      } finally {
        setLoading(false);
      }
    },
    [assetId, database, table, driver]
  );

  useEffect(() => {
    fetchData(page);
  }, [fetchData, page]);

  const hasNext = rows.length === PAGE_SIZE;
  const hasPrev = page > 0;

  return (
    <div className="flex flex-col h-full">
      {/* Header bar */}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b border-border bg-muted/30 shrink-0">
        <span className="text-xs font-mono font-semibold bg-muted px-1.5 py-0.5 rounded border border-border">
          {database}.{table}
        </span>
        {!loading && !error && (
          <span className="text-xs text-muted-foreground">
            {t("query.rows")}: {totalRows}
          </span>
        )}
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            disabled={!hasPrev || loading}
            onClick={() => setPage((p) => p - 1)}
            title={t("query.prevPage")}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>
          <span className="text-xs text-muted-foreground min-w-[60px] text-center">
            {t("query.page")} {page + 1}
          </span>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            disabled={!hasNext || loading}
            onClick={() => setPage((p) => p + 1)}
            title={t("query.nextPage")}
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Table content */}
      <QueryResultTable
        columns={columns}
        rows={rows}
        loading={loading}
        error={error ?? undefined}
      />
    </div>
  );
}
