import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input, Label } from "@opskat/ui";
import { AlertTriangle, Loader2, RefreshCw, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { notifySuccess } from "@/lib/notify";
import { failReasonLabel } from "@/lib/commandReview";
import { SecretInput } from "@/components/SecretInput";
import { Segmented } from "@/components/asset/fields";
import { ExtensionSettingsForm } from "@/components/settings/ExtensionSettingsForm";
import {
  GetCommandReviewSettings,
  SaveCommandReviewSettings,
  TestCommandReview,
} from "../../../wailsjs/go/system/System";
import { GetCommandReviewer, ListCommandReviewers, SetCommandReviewer } from "../../../wailsjs/go/extension/Extension";
import { extension_svc, system } from "../../../wailsjs/go/models";

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

// 审核服务选择里"内置审核"那一项；后端用空字符串表示。
const BUILTIN = "builtin";

// 与 internal/app/system/command_review.go 的校验一致。
const TIMEOUT_MIN = 1000;
const TIMEOUT_MAX = 60000;

interface FormState {
  baseUrl: string;
  model: string;
  timeoutMs: string;
  threshold: string;
}

const toForm = (s: system.CommandReviewSettings): FormState => ({
  baseUrl: s.baseUrl,
  model: s.model,
  timeoutMs: String(s.timeoutMs),
  threshold: String(s.threshold),
});

/**
 * 命令审核的设置：先选审核服务——内置审核或一个已安装的审核扩展。
 * 内置审核是 Jev 格式（TypeSafe System One API）的服务地址、API key（只写不读）、
 * 模型名（不做限制，用"测试模型"确认可用）、超时、阈值；审核扩展显示它自己声明的设置。
 * 两种都显示本次启动以来最近一次审核失败。放在设置 › AI 标签页。
 */
export function CommandReviewSection() {
  const { t } = useTranslation();
  const [loaded, setLoaded] = useState<system.CommandReviewSettings | null>(null);
  const [form, setForm] = useState<FormState>({ baseUrl: "", model: "", timeoutMs: "", threshold: "" });
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [reviewers, setReviewers] = useState<extension_svc.CommandReviewerInfo[]>([]);
  const [reviewer, setReviewer] = useState(BUILTIN);

  const apply = useCallback((s: system.CommandReviewSettings) => {
    setLoaded(s);
    setForm(toForm(s));
  }, []);
  const load = useCallback(() => GetCommandReviewSettings().then(apply), [apply]);

  useEffect(() => {
    GetCommandReviewSettings()
      .then(apply)
      .catch((e: unknown) => toast.error(errMsg(e)));
    Promise.all([ListCommandReviewers(), GetCommandReviewer()])
      .then(([list, selected]) => {
        setReviewers(list ?? []);
        setReviewer(selected || BUILTIN);
      })
      .catch((e: unknown) => toast.error(errMsg(e)));
  }, [apply]);

  const selectReviewer = async (next: string) => {
    if (next === reviewer) return;
    try {
      await SetCommandReviewer(next === BUILTIN ? "" : next);
      setReviewer(next);
      notifySuccess(t("commandReview.settings.reviewerSaved"));
    } catch (e: unknown) {
      toast.error(errMsg(e));
    }
  };
  const selectedExtension = reviewers.find((r) => r.name === reviewer);

  // 校验通过返回保存入参，否则提示并返回 null。
  const buildInput = (clearApiKey: boolean): system.CommandReviewSaveInput | null => {
    const timeoutMs = Number(form.timeoutMs);
    if (!Number.isInteger(timeoutMs) || timeoutMs < TIMEOUT_MIN || timeoutMs > TIMEOUT_MAX) {
      toast.error(t("commandReview.settings.timeoutRange", { min: TIMEOUT_MIN, max: TIMEOUT_MAX }));
      return null;
    }
    const threshold = Number(form.threshold);
    if (!Number.isFinite(threshold) || threshold <= 0 || threshold >= 1) {
      toast.error(t("commandReview.settings.thresholdRange"));
      return null;
    }
    return new system.CommandReviewSaveInput({
      apiKey: clearApiKey ? "" : apiKey.trim(),
      clearApiKey,
      baseUrl: form.baseUrl.trim(),
      model: form.model.trim(),
      timeoutMs,
      threshold,
    });
  };

  const save = async (clearApiKey = false): Promise<boolean> => {
    const input = buildInput(clearApiKey);
    if (!input) return false;
    setSaving(true);
    try {
      await SaveCommandReviewSettings(input);
      setApiKey("");
      await load();
      notifySuccess(t("commandReview.settings.saved"));
      return true;
    } catch (e: unknown) {
      toast.error(errMsg(e));
      return false;
    } finally {
      setSaving(false);
    }
  };

  // 用表单里填的值测试，不保存：先确认地址、key 和模型可用，再保存。API key 没重新填时用已保存的。
  const test = async () => {
    const input = buildInput(false);
    if (!input) return;
    setTesting(true);
    try {
      const model = await TestCommandReview(input);
      notifySuccess(t("commandReview.settings.testSuccess", { model }));
    } catch (e: unknown) {
      toast.error(errMsg(e));
    } finally {
      setTesting(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("commandReview.settings.title")}</CardTitle>
        <CardDescription>{t("commandReview.settings.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!loaded ? (
          <div className="flex justify-center py-4">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <>
            <div className="grid gap-1.5">
              <Label>{t("commandReview.settings.reviewer")}</Label>
              <Segmented
                value={reviewer}
                onChange={selectReviewer}
                aria-label={t("commandReview.settings.reviewer")}
                options={[
                  { value: BUILTIN, label: t("commandReview.settings.builtin"), testid: "command-reviewer-builtin" },
                  ...reviewers.map((r) => ({
                    value: r.name,
                    label:
                      r.usable && !r.signed
                        ? `${r.displayName} · ${t("commandReview.settings.unsigned")}`
                        : r.displayName,
                    disabled: !r.usable,
                    testid: `command-reviewer-${r.name}`,
                  })),
                ]}
              />
              <p className="text-xs text-muted-foreground">{t("commandReview.settings.reviewerHint")}</p>
              {reviewers
                .filter((r) => !r.usable)
                .map((r) => (
                  <p key={r.name} data-testid="command-reviewer-unusable" className="text-xs text-warning">
                    {t("commandReview.settings.reviewerUnusable", { name: r.displayName, reason: r.reason })}
                  </p>
                ))}
            </div>

            {selectedExtension ? (
              <div className="space-y-2" data-testid="command-reviewer-extension">
                <p className="text-xs text-muted-foreground">
                  {t("commandReview.settings.extensionTimeout", { timeout: loaded.timeoutMs })}
                </p>
                {selectedExtension.hasSettings && (
                  <ExtensionSettingsForm key={selectedExtension.name} extensionName={selectedExtension.name} />
                )}
              </div>
            ) : (
              <>
                <div className="grid gap-1.5">
                  <Label htmlFor="command-review-api-key">{t("commandReview.settings.apiKey")}</Label>
                  <SecretInput
                    id="command-review-api-key"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder={t("commandReview.settings.apiKeyPlaceholder")}
                    autoComplete="off"
                  />
                  <p className="text-xs text-muted-foreground">
                    {loaded.apiKeySet
                      ? t("commandReview.settings.apiKeySet")
                      : t("commandReview.settings.apiKeyNotSet")}
                  </p>
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="command-review-base-url">{t("commandReview.settings.baseUrl")}</Label>
                  <Input
                    id="command-review-base-url"
                    value={form.baseUrl}
                    onChange={(e) => setForm((f) => ({ ...f, baseUrl: e.target.value }))}
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <p className="text-xs text-muted-foreground">{t("commandReview.settings.baseUrlHint")}</p>
                </div>
                <div className="grid gap-3 sm:grid-cols-3">
                  <div className="grid gap-1.5">
                    <Label htmlFor="command-review-model">{t("commandReview.settings.model")}</Label>
                    <Input
                      id="command-review-model"
                      value={form.model}
                      onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="command-review-timeout">{t("commandReview.settings.timeout")}</Label>
                    <Input
                      id="command-review-timeout"
                      type="number"
                      min={TIMEOUT_MIN}
                      max={TIMEOUT_MAX}
                      step={500}
                      value={form.timeoutMs}
                      onChange={(e) => setForm((f) => ({ ...f, timeoutMs: e.target.value }))}
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="command-review-threshold">{t("commandReview.settings.threshold")}</Label>
                    <Input
                      id="command-review-threshold"
                      type="number"
                      min={0.01}
                      max={0.99}
                      step={0.05}
                      value={form.threshold}
                      onChange={(e) => setForm((f) => ({ ...f, threshold: e.target.value }))}
                    />
                  </div>
                </div>
                <div className="space-y-1 text-xs text-muted-foreground">
                  <p>{t("commandReview.settings.modelHint")}</p>
                  <p>{t("commandReview.settings.timeoutHint")}</p>
                  <p>{t("commandReview.settings.thresholdHint")}</p>
                </div>
              </>
            )}

            {loaded.lastFailReason && (
              <div
                data-testid="command-review-last-fail"
                className="flex items-start gap-1.5 rounded-md bg-warning/15 px-2.5 py-2 text-xs text-foreground"
              >
                <AlertTriangle aria-hidden className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
                <span>
                  {t("commandReview.settings.lastFail", {
                    reason: failReasonLabel(t, loaded.lastFailReason),
                    time: new Date(loaded.lastFailAt).toLocaleString(),
                  })}
                </span>
              </div>
            )}

            {!selectedExtension && (
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => save()} disabled={saving || testing} variant="outline" className="gap-1">
                  {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                  {t("commandReview.settings.save")}
                </Button>
                <Button
                  onClick={test}
                  disabled={saving || testing || (!loaded.apiKeySet && apiKey.trim() === "")}
                  variant="outline"
                  className="gap-1"
                >
                  {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                  {testing ? t("commandReview.settings.testing") : t("commandReview.settings.test")}
                </Button>
                {loaded.apiKeySet && (
                  <Button
                    onClick={() => save(true)}
                    disabled={saving || testing}
                    variant="ghost"
                    className="gap-1 text-muted-foreground"
                  >
                    <Trash2 className="h-4 w-4" />
                    {t("commandReview.settings.clearApiKey")}
                  </Button>
                )}
              </div>
            )}

            <p className="text-xs text-muted-foreground">{t("commandReview.settings.notice")}</p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
