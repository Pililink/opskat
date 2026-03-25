import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, X, Shield, Lock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ListPolicyGroups } from "../../../wailsjs/go/main/App";
import type { policy_group_entity } from "../../../wailsjs/go/models";

type PolicyType = "ssh" | "database" | "redis";

interface PolicyGroupSelectorProps {
  policyType: PolicyType;
  selectedIds: number[];
  onChange: (ids: number[]) => void;
  refreshKey?: number;
}

const policyTypeMap: Record<PolicyType, string> = {
  ssh: "command",
  database: "query",
  redis: "redis",
};

export function PolicyGroupSelector({ policyType, selectedIds, onChange, refreshKey }: PolicyGroupSelectorProps) {
  const { t } = useTranslation();
  const [groups, setGroups] = useState<policy_group_entity.PolicyGroupItem[]>([]);
  const [open, setOpen] = useState(false);

  const fetchGroups = async () => {
    try {
      const items = await ListPolicyGroups(policyTypeMap[policyType]);
      setGroups(items || []);
    } catch {
      setGroups([]);
    }
  };

  useEffect(() => {
    fetchGroups();
  }, [policyType, refreshKey]);

  const selectedGroups = groups.filter((g) => selectedIds.includes(g.id));
  const availableGroups = groups.filter((g) => !selectedIds.includes(g.id));

  const handleAdd = (id: number) => {
    onChange([...selectedIds, id]);
  };

  const handleRemove = (id: number) => {
    onChange(selectedIds.filter((i) => i !== id));
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5">
        <Shield className="h-3 w-3 text-indigo-500" />
        <span className="text-[11px] font-medium text-muted-foreground">{t("asset.policyGroup.referenced")}</span>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {selectedGroups.map((g) => (
          <span
            key={g.id}
            className="inline-flex items-center gap-1 rounded-md border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[11px] text-indigo-700 dark:border-indigo-800 dark:bg-indigo-950 dark:text-indigo-300"
          >
            {g.builtin && <Lock className="h-2.5 w-2.5" />}
            {g.name}
            <button
              onClick={() => handleRemove(g.id)}
              className="ml-0.5 rounded-sm hover:bg-indigo-200 dark:hover:bg-indigo-800"
            >
              <X className="h-2.5 w-2.5" />
            </button>
          </span>
        ))}

        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="sm" className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground">
              <Plus className="h-3 w-3" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-56 p-1" align="start">
            {availableGroups.length === 0 ? (
              <div className="px-2 py-3 text-center text-xs text-muted-foreground">{t("asset.policyGroup.noMore")}</div>
            ) : (
              <div className="max-h-48 overflow-y-auto">
                {availableGroups.map((g) => (
                  <button
                    key={g.id}
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs hover:bg-accent"
                    onClick={() => {
                      handleAdd(g.id);
                      setOpen(false);
                    }}
                  >
                    {g.builtin && <Lock className="h-3 w-3 text-muted-foreground shrink-0" />}
                    <div className="flex-1 text-left">
                      <div className="font-medium">{g.name}</div>
                      {g.description && <div className="text-[10px] text-muted-foreground">{g.description}</div>}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
