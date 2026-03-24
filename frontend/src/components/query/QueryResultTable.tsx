import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";

interface QueryResultTableProps {
  columns: string[];
  rows: Record<string, unknown>[];
  loading?: boolean;
  error?: string;
}

export function QueryResultTable({
  columns,
  rows,
  loading,
  error,
}: QueryResultTableProps) {
  const { t } = useTranslation();

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="px-3 py-4 text-xs text-destructive whitespace-pre-wrap font-mono">
        {error}
      </div>
    );
  }

  if (columns.length === 0) {
    return (
      <div className="flex items-center justify-center py-8 text-xs text-muted-foreground">
        {t("query.noResult")}
      </div>
    );
  }

  return (
    <div className="overflow-auto flex-1 min-h-0">
      <table className="w-full border-collapse text-xs font-mono">
        <thead className="sticky top-0 z-10 bg-muted">
          <tr>
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
            <tr
              key={idx}
              className={idx % 2 === 0 ? "bg-background" : "bg-muted/40"}
            >
              {columns.map((col) => (
                <td
                  key={col}
                  className="border border-border px-2 py-1 whitespace-nowrap max-w-[400px] truncate"
                  title={row[col] == null ? "NULL" : String(row[col])}
                >
                  {row[col] == null ? (
                    <span className="text-muted-foreground italic">NULL</span>
                  ) : (
                    String(row[col])
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
