import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  ChevronRight,
  ChevronDown,
  Database,
  Table2,
  Plus,
  RefreshCw,
  Loader2,
  AlertCircle,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useQueryStore } from "@/stores/queryStore";

interface DatabaseTreeProps {
  tabId: string;
}

export function DatabaseTree({ tabId }: DatabaseTreeProps) {
  const { t } = useTranslation();
  const { dbStates, loadDatabases, toggleDbExpand, openTableTab, openSqlTab } =
    useQueryStore();

  const dbState = dbStates[tabId];

  useEffect(() => {
    loadDatabases(tabId);
  }, [tabId, loadDatabases]);

  if (!dbState) return null;

  const { databases, tables, expandedDbs, loadingDbs, error } = dbState;

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-2 py-1.5 border-b border-border shrink-0">
        <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {t("query.databases")}
        </span>
        <div className="flex gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            onClick={() => openSqlTab(tabId)}
            title={t("query.newSql")}
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            onClick={() => loadDatabases(tabId)}
            title={t("query.refreshTree")}
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Error message */}
      {error && (
        <div className="flex items-start gap-2 border-b border-destructive/20 bg-destructive/10 px-2 py-2 text-xs text-destructive">
          <AlertCircle className="size-3.5 shrink-0 mt-0.5" />
          <span className="break-all">{error}</span>
        </div>
      )}

      {/* Tree */}
      <ScrollArea className="flex-1 min-h-0">
        <div className="p-1 space-y-0.5">
          {loadingDbs ? (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            </div>
          ) : databases.length === 0 ? (
            <div className="text-xs text-muted-foreground text-center py-4">
              {t("query.databases")}
            </div>
          ) : (
            databases.map((db) => {
              const isExpanded = expandedDbs.has(db);
              const dbTables = tables[db];

              return (
                <div key={db}>
                  {/* Database node */}
                  <div
                    className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs cursor-pointer hover:bg-accent transition-colors duration-150"
                    onClick={() => toggleDbExpand(tabId, db)}
                  >
                    {isExpanded ? (
                      <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
                    )}
                    <Database className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate">{db}</span>
                  </div>

                  {/* Tables */}
                  {isExpanded && (
                    <div className="ml-3">
                      {!dbTables ? (
                        <div className="flex items-center gap-1.5 px-2 py-1">
                          <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                        </div>
                      ) : dbTables.length === 0 ? (
                        <div className="px-2 py-1 text-xs text-muted-foreground italic">
                          {t("query.noTables")}
                        </div>
                      ) : (
                        dbTables.map((tbl) => (
                          <div
                            key={tbl}
                            className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs cursor-pointer hover:bg-accent transition-colors duration-150"
                            onDoubleClick={() =>
                              openTableTab(tabId, db, tbl)
                            }
                          >
                            <Table2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                            <span className="truncate">{tbl}</span>
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
