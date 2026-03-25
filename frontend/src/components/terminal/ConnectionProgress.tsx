import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, RotateCcw, X, Check, KeyRound, Server, Shield, TerminalSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useTerminalStore, type ConnectionState, type ConnectionStep } from "@/stores/terminalStore";

const STEPS: { key: ConnectionStep; icon: typeof Server }[] = [
  { key: "resolve", icon: KeyRound },
  { key: "connect", icon: Server },
  { key: "auth", icon: Shield },
  { key: "shell", icon: TerminalSquare },
];

const STEP_ORDER: ConnectionStep[] = ["resolve", "connect", "auth", "shell"];

function getStepIndex(step: ConnectionStep): number {
  return STEP_ORDER.indexOf(step);
}

interface ConnectionProgressProps {
  connectionId: string;
}

export function ConnectionProgress({ connectionId }: ConnectionProgressProps) {
  const { t } = useTranslation();
  const connection = useTerminalStore((s) => s.connections[connectionId]) as ConnectionState | undefined;
  const retryConnect = useTerminalStore((s) => s.retryConnect);
  const respondChallenge = useTerminalStore((s) => s.respondChallenge);
  const cancelConnect = useTerminalStore((s) => s.cancelConnect);

  if (!connection) {
    return (
      <div className="h-full w-full flex items-center justify-center bg-background">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const isError = connection.status === "error";
  const isChallenge = connection.status === "auth_challenge";
  const currentIdx = getStepIndex(connection.currentStep);

  return (
    <div className="h-full w-full flex flex-col bg-background">
      {/* Status area */}
      <div className="flex-1 flex flex-col items-center justify-center px-6 min-h-0">
        {/* Asset name */}
        <div className="text-sm text-muted-foreground mb-6">{connection.assetName}</div>

        {/* Step progress */}
        <div className="flex items-center gap-0 mb-6 w-full max-w-xs">
          {STEPS.map((step, i) => {
            const Icon = step.icon;
            const stepIdx = getStepIndex(step.key);
            const isCurrent = stepIdx === currentIdx;
            const isDone = stepIdx < currentIdx;
            const isFailed = isError && isCurrent;
            const isWaiting = isChallenge && isCurrent;

            return (
              <div key={step.key} className="flex items-center flex-1 last:flex-none">
                {/* Step circle */}
                <div
                  className={`
                    relative flex items-center justify-center w-9 h-9 rounded-full border-2 transition-all duration-300 shrink-0
                    ${
                      isFailed
                        ? "border-destructive bg-destructive/10"
                        : isWaiting
                          ? "border-yellow-500 bg-yellow-500/10"
                          : isDone
                            ? "border-primary bg-primary text-primary-foreground"
                            : isCurrent
                              ? "border-primary bg-primary/10"
                              : "border-muted bg-muted/30"
                    }
                  `}
                >
                  {isDone ? (
                    <Check className="h-4 w-4" />
                  ) : isCurrent && !isFailed && !isWaiting ? (
                    <Loader2 className="h-4 w-4 animate-spin text-primary" />
                  ) : (
                    <Icon
                      className={`h-4 w-4 ${
                        isFailed
                          ? "text-destructive"
                          : isWaiting
                            ? "text-yellow-500"
                            : isCurrent
                              ? "text-primary"
                              : "text-muted-foreground/50"
                      }`}
                    />
                  )}
                  {isCurrent && !isFailed && !isWaiting && (
                    <span className="absolute inset-0 rounded-full border-2 border-primary animate-ping opacity-30" />
                  )}
                </div>

                {/* Connector line */}
                {i < STEPS.length - 1 && (
                  <div className="flex-1 h-0.5 mx-1">
                    <div
                      className={`h-full rounded-full transition-all duration-500 ${
                        isDone ? "bg-primary" : "bg-muted"
                      }`}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {/* Step labels */}
        <div className="flex items-start gap-0 w-full max-w-xs mb-8">
          {STEPS.map((step) => {
            const stepIdx = getStepIndex(step.key);
            const isCurrent = stepIdx === currentIdx;
            const isDone = stepIdx < currentIdx;
            return (
              <div key={step.key} className="flex-1 last:flex-none last:w-9 text-center">
                <span
                  className={`text-[11px] leading-tight ${
                    isCurrent || isDone ? "text-foreground" : "text-muted-foreground/50"
                  }`}
                >
                  {t(`ssh.connectProgress.steps.${step.key}`)}
                </span>
              </div>
            );
          })}
        </div>

        {/* Status message */}
        <div className="text-center mb-4">
          {connection.status === "connecting" && connection.logs.length > 0 && (
            <p className="text-sm text-muted-foreground">{connection.logs[connection.logs.length - 1].message}</p>
          )}
          {isError && <p className="text-sm text-destructive">{connection.error}</p>}
          {isChallenge && <p className="text-sm text-yellow-500">{t("ssh.connectProgress.authChallenge")}</p>}
        </div>

        {/* Inline auth challenge form */}
        {isChallenge && connection.challenge && (
          <AuthChallengeForm
            prompts={connection.challenge.prompts}
            echo={connection.challenge.echo}
            onSubmit={(answers) => respondChallenge(connectionId, answers)}
          />
        )}

        {/* Error actions */}
        {isError && (
          <ErrorActions
            authFailed={connection.authFailed || false}
            onRetry={(password) => retryConnect(connectionId, password)}
            onClose={() => cancelConnect(connectionId)}
          />
        )}

        {/* Cancel button (when connecting) */}
        {connection.status === "connecting" && (
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground"
            onClick={() => cancelConnect(connectionId)}
          >
            <X className="h-3.5 w-3.5 mr-1" />
            {t("action.cancel")}
          </Button>
        )}
      </div>

      {/* Log area (collapsible bottom) */}
      {connection.logs.length > 0 && <LogArea logs={connection.logs} />}
    </div>
  );
}

function LogArea({ logs }: { logs: ConnectionState["logs"] }) {
  const logEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs.length]);

  const formatTime = (ts: number) => {
    const d = new Date(ts);
    return `${d.getHours().toString().padStart(2, "0")}:${d
      .getMinutes()
      .toString()
      .padStart(2, "0")}:${d.getSeconds().toString().padStart(2, "0")}`;
  };

  return (
    <div className="border-t max-h-28 overflow-auto px-4 py-2 bg-muted/30 font-mono text-xs">
      {logs.map((log, i) => (
        <div key={i} className="flex items-start gap-2 py-px">
          <span className="text-muted-foreground/60 shrink-0">{formatTime(log.timestamp)}</span>
          <span className={log.type === "error" ? "text-destructive" : "text-muted-foreground"}>{log.message}</span>
        </div>
      ))}
      <div ref={logEndRef} />
    </div>
  );
}

function AuthChallengeForm({
  prompts,
  echo,
  onSubmit,
}: {
  prompts: string[];
  echo: boolean[];
  onSubmit: (answers: string[]) => void;
}) {
  const { t } = useTranslation();
  const [answers, setAnswers] = useState<string[]>(() => new Array(prompts.length).fill(""));
  const handleSubmit = () => {
    onSubmit(answers);
  };

  return (
    <div className="w-full max-w-xs space-y-3 mb-4">
      {prompts.map((prompt, i) => (
        <div key={i} className="space-y-1">
          <label className="text-xs text-muted-foreground">{prompt}</label>
          <Input
            type={echo[i] ? "text" : "password"}
            value={answers[i]}
            onChange={(e) => {
              const next = [...answers];
              next[i] = e.target.value;
              setAnswers(next);
            }}
            onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            className="h-8 text-sm"
            autoFocus={i === 0}
          />
        </div>
      ))}
      <Button size="sm" onClick={handleSubmit} className="w-full">
        {t("action.submit")}
      </Button>
    </div>
  );
}

function ErrorActions({
  authFailed,
  onRetry,
  onClose,
}: {
  authFailed: boolean;
  onRetry: (password?: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");

  return (
    <div className="w-full max-w-xs space-y-3 mb-4">
      {authFailed && (
        <div className="space-y-1">
          <label className="text-xs text-muted-foreground">{t("ssh.password")}</label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && onRetry(password || undefined)}
            className="h-8 text-sm"
            autoFocus
          />
        </div>
      )}
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={onClose} className="flex-1">
          <X className="h-3.5 w-3.5 mr-1" />
          {t("action.close")}
        </Button>
        <Button size="sm" onClick={() => onRetry(authFailed ? password || undefined : undefined)} className="flex-1">
          <RotateCcw className="h-3.5 w-3.5 mr-1" />
          {t("ssh.connectProgress.retry")}
        </Button>
      </div>
    </div>
  );
}
