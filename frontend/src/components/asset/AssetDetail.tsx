import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Server, Database, Pencil, Trash2, TerminalSquare, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import { cn } from "@/lib/utils";
import { useAssetStore } from "@/stores/assetStore";
import { asset_entity } from "../../../wailsjs/go/models";

interface SSHConfig {
  host: string;
  port: number;
  username: string;
  auth_type: string;
  password?: string;
  credential_id?: number;
  private_keys?: string[];
  jump_host_id?: number;
  proxy?: {
    type: string;
    host: string;
    port: number;
    username?: string;
    password?: string;
  } | null;
}

interface DatabaseConfig {
  driver: string;
  host: string;
  port: number;
  username: string;
  password?: string;
  database?: string;
  ssl_mode?: string;
  params?: string;
  read_only?: boolean;
  ssh_asset_id?: number;
}

interface RedisConfig {
  host: string;
  port: number;
  username?: string;
  password?: string;
  database?: number;
  tls?: boolean;
  ssh_asset_id?: number;
}

interface AssetDetailProps {
  asset: asset_entity.Asset;
  isConnecting?: boolean;
  onEdit: () => void;
  onDelete: () => void;
  onConnect: () => void;
}

export function AssetDetail({ asset, isConnecting, onEdit, onDelete, onConnect }: AssetDetailProps) {
  const { t } = useTranslation();
  const { assets, updateAsset } = useAssetStore();
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [savingPolicy, setSavingPolicy] = useState(false);

  // SSH Command policy
  const [allowList, setAllowList] = useState<string[]>([]);
  const [denyList, setDenyList] = useState<string[]>([]);
  const [allowInput, setAllowInput] = useState("");
  const [denyInput, setDenyInput] = useState("");

  // Database Query policy
  const [queryAllowTypes, setQueryAllowTypes] = useState<string[]>([]);
  const [queryDenyTypes, setQueryDenyTypes] = useState<string[]>([]);
  const [queryDenyFlags, setQueryDenyFlags] = useState<string[]>([]);
  const [queryAllowInput, setQueryAllowInput] = useState("");
  const [queryDenyInput, setQueryDenyInput] = useState("");
  const [queryFlagInput, setQueryFlagInput] = useState("");

  // Redis policy
  const [redisAllowList, setRedisAllowList] = useState<string[]>([]);
  const [redisDenyList, setRedisDenyList] = useState<string[]>([]);
  const [redisAllowInput, setRedisAllowInput] = useState("");
  const [redisDenyInput, setRedisDenyInput] = useState("");

  useEffect(() => {
    try {
      const policy = JSON.parse(asset.CmdPolicy || "{}");
      if (asset.Type === "database") {
        setQueryAllowTypes(policy.allow_types || []);
        setQueryDenyTypes(policy.deny_types || []);
        setQueryDenyFlags(policy.deny_flags || []);
      } else if (asset.Type === "redis") {
        setRedisAllowList(policy.allow_list || []);
        setRedisDenyList(policy.deny_list || []);
      } else {
        setAllowList(policy.allow_list || []);
        setDenyList(policy.deny_list || []);
      }
    } catch {
      setAllowList([]); setDenyList([]);
      setQueryAllowTypes([]); setQueryDenyTypes([]); setQueryDenyFlags([]);
      setRedisAllowList([]); setRedisDenyList([]);
    }
    setAllowInput(""); setDenyInput("");
    setQueryAllowInput(""); setQueryDenyInput(""); setQueryFlagInput("");
    setRedisAllowInput(""); setRedisDenyInput("");
  }, [asset.ID, asset.CmdPolicy, asset.Type]);

  const savePolicy = async (policyObj: Record<string, unknown>) => {
    // Remove empty arrays
    const cleaned: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(policyObj)) {
      if (Array.isArray(v) && v.length > 0) cleaned[k] = v;
    }
    const cmdPolicy = Object.keys(cleaned).length > 0 ? JSON.stringify(cleaned) : "";
    const updated = new asset_entity.Asset({ ...asset, CmdPolicy: cmdPolicy });
    setSavingPolicy(true);
    try {
      await updateAsset(updated);
    } catch (e) {
      toast.error(String(e));
    } finally {
      setSavingPolicy(false);
    }
  };

  const handleSaveSSHPolicy = async (newAllow: string[], newDeny: string[]) => {
    await savePolicy({ allow_list: newAllow, deny_list: newDeny });
  };

  const handleSaveQueryPolicy = async (newAllowTypes: string[], newDenyTypes: string[], newDenyFlags: string[]) => {
    await savePolicy({ allow_types: newAllowTypes, deny_types: newDenyTypes, deny_flags: newDenyFlags });
  };

  const handleSaveRedisPolicy = async (newAllow: string[], newDeny: string[]) => {
    await savePolicy({ allow_list: newAllow, deny_list: newDeny });
  };

  // Parse config based on type
  let sshConfig: SSHConfig | null = null;
  let dbConfig: DatabaseConfig | null = null;
  let redisConfig: RedisConfig | null = null;
  try {
    const parsed = JSON.parse(asset.Config || "{}");
    if (asset.Type === "database") dbConfig = parsed;
    else if (asset.Type === "redis") redisConfig = parsed;
    else sshConfig = parsed;
  } catch { /* ignore */ }

  const jumpHostName = sshConfig?.jump_host_id
    ? assets.find((a) => a.ID === sshConfig!.jump_host_id)?.Name || `ID:${sshConfig.jump_host_id}`
    : null;

  const sshTunnelName = (id?: number) => {
    if (!id) return null;
    return assets.find((a) => a.ID === id)?.Name || `ID:${id}`;
  };

  const HeaderIcon = asset.Type === "database" ? Database : Server;

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-4 py-3 border-b">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10">
            <HeaderIcon className="h-4 w-4 text-primary" />
          </div>
          <div>
            <h2 className="font-semibold leading-tight">{asset.Name}</h2>
            <span className="text-xs text-muted-foreground uppercase">
              {asset.Type}
            </span>
          </div>
        </div>
        <div className="flex gap-1.5">
          {asset.Type === "ssh" && (
            <Button size="sm" className="h-8 gap-1.5" onClick={onConnect} disabled={isConnecting}>
              {isConnecting ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <TerminalSquare className="h-3.5 w-3.5" />
              )}
              {t("ssh.connect")}
            </Button>
          )}
          <Button variant="ghost" size="icon" className="h-8 w-8" onClick={onEdit}>
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 text-destructive hover:text-destructive"
            onClick={() => setShowDeleteConfirm(true)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
      <AlertDialog open={showDeleteConfirm} onOpenChange={setShowDeleteConfirm}>
        <AlertDialogContent onOverlayClick={() => setShowDeleteConfirm(false)}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("asset.deleteAssetTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("asset.deleteAssetDesc", { name: asset.Name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("action.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={onDelete}>
              {t("action.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <div className="flex-1 p-4 space-y-4 overflow-y-auto">
        {/* SSH Connection Info */}
        {sshConfig && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              SSH Connection
            </h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <InfoItem label={t("asset.host")} value={sshConfig.host} mono />
              <InfoItem label={t("asset.port")} value={String(sshConfig.port)} mono />
              <InfoItem label={t("asset.username")} value={sshConfig.username} mono />
              <InfoItem
                label={t("asset.authType")}
                value={
                  sshConfig.auth_type === "password"
                    ? t("asset.authPassword") + (sshConfig.password ? " ●" : "")
                    : sshConfig.auth_type === "key"
                    ? t("asset.authKey") + (sshConfig.credential_id ? ` (${t("asset.keySourceManaged")})` : sshConfig.private_keys?.length ? ` (${t("asset.keySourceFile")})` : "")
                    : sshConfig.auth_type
                }
              />
            </div>
          </div>
        )}

        {/* SSH Private Keys */}
        {sshConfig?.private_keys && sshConfig.private_keys.length > 0 && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.privateKeys")}
            </h3>
            <div className="space-y-1">
              {sshConfig.private_keys.map((key, i) => (
                <p key={i} className="text-sm font-mono text-muted-foreground">{key}</p>
              ))}
            </div>
          </div>
        )}

        {/* SSH Jump Host */}
        {jumpHostName && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.jumpHost")}
            </h3>
            <p className="text-sm font-mono">{jumpHostName}</p>
          </div>
        )}

        {/* SSH Proxy */}
        {sshConfig?.proxy && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.proxy")}
            </h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <InfoItem label={t("asset.proxyType")} value={sshConfig.proxy.type.toUpperCase()} />
              <InfoItem
                label={t("asset.proxyHost")}
                value={`${sshConfig.proxy.host}:${sshConfig.proxy.port}`}
                mono
              />
              {sshConfig.proxy.username && (
                <InfoItem label={t("asset.proxyUsername")} value={sshConfig.proxy.username} />
              )}
            </div>
          </div>
        )}

        {/* Database Connection Info */}
        {dbConfig && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.typeDatabase")}
            </h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <InfoItem label={t("asset.driver")} value={dbConfig.driver === "postgresql" ? "PostgreSQL" : "MySQL"} />
              <InfoItem label={t("asset.host")} value={`${dbConfig.host}:${dbConfig.port}`} mono />
              <InfoItem label={t("asset.username")} value={dbConfig.username} mono />
              {dbConfig.database && (
                <InfoItem label={t("asset.database")} value={dbConfig.database} mono />
              )}
              {dbConfig.password && (
                <InfoItem label={t("asset.password")} value="●●●●●●" />
              )}
              {dbConfig.ssl_mode && dbConfig.ssl_mode !== "disable" && (
                <InfoItem label={t("asset.sslMode")} value={dbConfig.ssl_mode} />
              )}
              {dbConfig.read_only && (
                <InfoItem label={t("asset.readOnly")} value="✓" />
              )}
              {dbConfig.params && (
                <InfoItem label={t("asset.params")} value={dbConfig.params} mono />
              )}
            </div>
            {sshTunnelName(dbConfig.ssh_asset_id) && (
              <div className="mt-3 pt-3 border-t text-sm">
                <InfoItem label={t("asset.sshTunnel")} value={sshTunnelName(dbConfig.ssh_asset_id)!} mono />
              </div>
            )}
          </div>
        )}

        {/* Redis Connection Info */}
        {redisConfig && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              Redis
            </h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <InfoItem label={t("asset.host")} value={`${redisConfig.host}:${redisConfig.port}`} mono />
              {redisConfig.username && (
                <InfoItem label={t("asset.username")} value={redisConfig.username} mono />
              )}
              {redisConfig.password && (
                <InfoItem label={t("asset.password")} value="●●●●●●" />
              )}
              <InfoItem label={t("asset.redisDatabase")} value={String(redisConfig.database || 0)} mono />
              {redisConfig.tls && (
                <InfoItem label={t("asset.tls")} value="✓" />
              )}
            </div>
            {sshTunnelName(redisConfig.ssh_asset_id) && (
              <div className="mt-3 pt-3 border-t text-sm">
                <InfoItem label={t("asset.sshTunnel")} value={sshTunnelName(redisConfig.ssh_asset_id)!} mono />
              </div>
            )}
          </div>
        )}

        {/* SSH Command Policy */}
        {asset.Type === "ssh" && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.cmdPolicy")}
            </h3>
            <PolicyTagEditor
              label={t("asset.cmdPolicyAllowList")}
              items={allowList}
              input={allowInput}
              onInputChange={setAllowInput}
              onAdd={(val) => { const next = [...allowList, val]; setAllowList(next); handleSaveSSHPolicy(next, denyList); }}
              onRemove={(i) => { const next = allowList.filter((_, idx) => idx !== i); setAllowList(next); handleSaveSSHPolicy(next, denyList); }}
              placeholder={t("asset.cmdPolicyPlaceholder")}
              color="green"
            />
            <PolicyTagEditor
              label={t("asset.cmdPolicyDenyList")}
              items={denyList}
              input={denyInput}
              onInputChange={setDenyInput}
              onAdd={(val) => { const next = [...denyList, val]; setDenyList(next); handleSaveSSHPolicy(allowList, next); }}
              onRemove={(i) => { const next = denyList.filter((_, idx) => idx !== i); setDenyList(next); handleSaveSSHPolicy(allowList, next); }}
              placeholder={t("asset.cmdPolicyPlaceholder")}
              color="red"
            />
            <p className="text-xs text-muted-foreground">
              {savingPolicy ? t("settings.saved") + "..." : t("asset.cmdPolicyHint")}
            </p>
          </div>
        )}

        {/* Database Query Policy */}
        {asset.Type === "database" && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.queryPolicy")}
            </h3>
            <PolicyTagEditor
              label={t("asset.queryPolicyAllowTypes")}
              items={queryAllowTypes}
              input={queryAllowInput}
              onInputChange={setQueryAllowInput}
              onAdd={(val) => { const next = [...queryAllowTypes, val]; setQueryAllowTypes(next); handleSaveQueryPolicy(next, queryDenyTypes, queryDenyFlags); }}
              onRemove={(i) => { const next = queryAllowTypes.filter((_, idx) => idx !== i); setQueryAllowTypes(next); handleSaveQueryPolicy(next, queryDenyTypes, queryDenyFlags); }}
              placeholder={t("asset.queryPolicyPlaceholder")}
              color="green"
            />
            <PolicyTagEditor
              label={t("asset.queryPolicyDenyTypes")}
              items={queryDenyTypes}
              input={queryDenyInput}
              onInputChange={setQueryDenyInput}
              onAdd={(val) => { const next = [...queryDenyTypes, val]; setQueryDenyTypes(next); handleSaveQueryPolicy(queryAllowTypes, next, queryDenyFlags); }}
              onRemove={(i) => { const next = queryDenyTypes.filter((_, idx) => idx !== i); setQueryDenyTypes(next); handleSaveQueryPolicy(queryAllowTypes, next, queryDenyFlags); }}
              placeholder={t("asset.queryPolicyPlaceholder")}
              color="red"
            />
            <PolicyTagEditor
              label={t("asset.queryPolicyDenyFlags")}
              items={queryDenyFlags}
              input={queryFlagInput}
              onInputChange={setQueryFlagInput}
              onAdd={(val) => { const next = [...queryDenyFlags, val]; setQueryDenyFlags(next); handleSaveQueryPolicy(queryAllowTypes, queryDenyTypes, next); }}
              onRemove={(i) => { const next = queryDenyFlags.filter((_, idx) => idx !== i); setQueryDenyFlags(next); handleSaveQueryPolicy(queryAllowTypes, queryDenyTypes, next); }}
              placeholder={t("asset.queryPolicyFlagPlaceholder")}
              color="orange"
            />
            <p className="text-xs text-muted-foreground">
              {savingPolicy ? t("settings.saved") + "..." : t("asset.queryPolicyHint")}
            </p>
          </div>
        )}

        {/* Redis Policy */}
        {asset.Type === "redis" && (
          <div className="rounded-xl border bg-card p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
              {t("asset.redisPolicy")}
            </h3>
            <PolicyTagEditor
              label={t("asset.redisPolicyAllowList")}
              items={redisAllowList}
              input={redisAllowInput}
              onInputChange={setRedisAllowInput}
              onAdd={(val) => { const next = [...redisAllowList, val]; setRedisAllowList(next); handleSaveRedisPolicy(next, redisDenyList); }}
              onRemove={(i) => { const next = redisAllowList.filter((_, idx) => idx !== i); setRedisAllowList(next); handleSaveRedisPolicy(next, redisDenyList); }}
              placeholder={t("asset.redisPolicyPlaceholder")}
              color="green"
            />
            <PolicyTagEditor
              label={t("asset.redisPolicyDenyList")}
              items={redisDenyList}
              input={redisDenyInput}
              onInputChange={setRedisDenyInput}
              onAdd={(val) => { const next = [...redisDenyList, val]; setRedisDenyList(next); handleSaveRedisPolicy(redisAllowList, next); }}
              onRemove={(i) => { const next = redisDenyList.filter((_, idx) => idx !== i); setRedisDenyList(next); handleSaveRedisPolicy(redisAllowList, next); }}
              placeholder={t("asset.redisPolicyPlaceholder")}
              color="red"
            />
            <p className="text-xs text-muted-foreground">
              {savingPolicy ? t("settings.saved") + "..." : t("asset.redisPolicyHint")}
            </p>
          </div>
        )}

        {asset.Description && (
          <>
            <Separator />
            <div className="text-sm">
              <span className="text-muted-foreground">
                {t("asset.description")}
              </span>
              <p className="mt-1">{asset.Description}</p>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

const TAG_COLORS: Record<string, string> = {
  green: "bg-green-500/10 text-green-600",
  red: "bg-red-500/10 text-red-600",
  orange: "bg-orange-500/10 text-orange-600",
};

function PolicyTagEditor({
  label,
  items,
  input,
  onInputChange,
  onAdd,
  onRemove,
  placeholder,
  color,
}: {
  label: string;
  items: string[];
  input: string;
  onInputChange: (v: string) => void;
  onAdd: (val: string) => void;
  onRemove: (idx: number) => void;
  placeholder: string;
  color: string;
}) {
  return (
    <div className="grid gap-2 mb-3">
      <Label className="text-xs">{label}</Label>
      <div className="flex flex-wrap gap-1.5 min-h-[24px]">
        {items.map((item, i) => (
          <span
            key={i}
            className={cn("inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-mono", TAG_COLORS[color])}
          >
            {item}
            <button
              type="button"
              className="hover:text-destructive"
              onClick={() => onRemove(i)}
            >
              x
            </button>
          </span>
        ))}
      </div>
      <Input
        className="h-7 text-xs font-mono"
        value={input}
        onChange={(e) => onInputChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && input.trim()) {
            e.preventDefault();
            onAdd(input.trim());
            onInputChange("");
          }
        }}
        placeholder={placeholder}
      />
    </div>
  );
}

function InfoItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <span className="text-xs text-muted-foreground">{label}</span>
      <p className={cn("mt-0.5 text-sm", mono && "font-mono")}>{value}</p>
    </div>
  );
}
