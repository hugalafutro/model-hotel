import { useCallback, useState } from "react";
import {
  Copy,
  Check,
  Clock,
  Hash,
  Box,
  Server,
  Activity,
  Key,
  AlertTriangle,
  Layers,
  Gauge,
  Timer,
  Zap,
  FileText,
  Tag,
  Calendar,
} from "lucide-react";
import { Modal } from "./Modal";
import type { LogEntry, AppLogEntry } from "../api/types";

interface LogDetailModalProps {
  log: LogEntry | AppLogEntry | null;
  type: "request" | "app";
  onClose: () => void;
}

function isRequestLog(log: LogEntry | AppLogEntry): log is LogEntry {
  return "request_id" in log;
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
  } catch {
    return iso;
  }
}

function formatDuration(ms: number): string {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`;
  }
  return `${Math.round(ms)}ms`;
}

function DetailItem({
  icon: Icon,
  label,
  value,
  mono = false,
  accent = false,
  truncate = false,
  children,
}: {
  icon: typeof Clock;
  label: string;
  value?: string | number | null;
  mono?: boolean;
  accent?: boolean;
  truncate?: boolean;
  children?: React.ReactNode;
}) {
  const displayValue =
    value === null || value === undefined || value === "" ? "—" : value;

  return (
    <div className="flex items-start gap-3 p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle)">
      <div className="shrink-0 mt-0.5">
        <Icon
          size={16}
          className={accent ? "text-(--accent)" : "text-(--text-tertiary)"}
        />
      </div>
      <div className="flex-1 min-w-0">
        <div className="text-[11px] uppercase tracking-wider text-(--text-tertiary) font-medium mb-1">
          {label}
        </div>
        {children ? (
          children
        ) : (
          <div
            className={`text-sm text-(--text-primary) ${mono ? "font-mono" : ""} ${truncate ? "truncate" : "wrap-break-words"}`}
          >
            {displayValue}
          </div>
        )}
      </div>
    </div>
  );
}

function StatusBadge({
  code,
  state,
  errorMessage,
}: {
  code: number;
  state: string;
  errorMessage?: string;
}) {
  if (state === "pending" || state === "streaming") {
    return (
      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-blue-500/15 text-blue-400 border border-blue-500/30">
        <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
        {state === "streaming" ? "Streaming" : "Pending"}
      </span>
    );
  }

  if (code === 0) {
    return (
      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">
        <AlertTriangle size={12} />
        Failed{errorMessage ? `: ${errorMessage}` : ""}
      </span>
    );
  }

  if (code >= 200 && code < 300) {
    return (
      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-green-500/15 text-green-400 border border-green-500/30">
        <Activity size={12} />
        {code} OK
      </span>
    );
  }

  if (code >= 400 && code < 500) {
    return (
      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-orange-500/15 text-orange-400 border border-orange-500/30">
        <AlertTriangle size={12} />
        {code} Client Error
      </span>
    );
  }

  if (code >= 500) {
    return (
      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">
        <AlertTriangle size={12} />
        {code} Server Error
      </span>
    );
  }

  return <span className="text-xs text-(--text-secondary)">{code}</span>;
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [text]);

  return (
    <button
      onClick={handleCopy}
      className="ml-2 text-(--text-tertiary) hover:text-(--accent) transition-colors cursor-default"
      title="Copy to clipboard"
    >
      {copied ? (
        <Check size={14} className="text-green-400" />
      ) : (
        <Copy size={14} />
      )}
    </button>
  );
}

export function LogDetailModal({ log, type, onClose }: LogDetailModalProps) {
  if (!log) return null;

  if (type === "request" && isRequestLog(log)) {
    const requestLog = log as LogEntry;
    const totalTokens = requestLog.tokens_prompt + requestLog.tokens_completion;
    const hasCache =
      requestLog.tokens_prompt_cache_hit > 0 ||
      requestLog.tokens_prompt_cache_miss > 0;

    return (
      <Modal
        title="Request Details"
        onClose={onClose}
        maxWidth="max-w-2xl"
        scrollable
      >
        {/* Header Section */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <StatusBadge
              code={requestLog.status_code}
              state={requestLog.state}
              errorMessage={requestLog.error_message}
            />
            {requestLog.failover_attempt > 0 && (
              <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-purple-500/15 text-purple-400 border border-purple-500/30">
                <Layers size={12} />
                Attempt {requestLog.failover_attempt + 1}
              </span>
            )}
          </div>
          <div className="text-xs text-(--text-tertiary) font-mono">
            ID: {requestLog.id.slice(0, 12)}…
            <CopyButton text={requestLog.id} />
          </div>
        </div>

        {/* Timing Overview */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
          <div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
            <Clock size={16} className="mx-auto mb-1 text-(--accent)" />
            <div className="text-lg font-bold text-(--text-primary)">
              {formatDuration(requestLog.duration_ms)}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
              Duration
            </div>
          </div>
          <div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
            <Timer size={16} className="mx-auto mb-1 text-(--accent)" />
            <div className="text-lg font-bold text-(--text-primary)">
              {requestLog.ttft_ms > 0
                ? formatDuration(requestLog.ttft_ms)
                : "—"}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
              TTFT
            </div>
          </div>
          <div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
            <Zap size={16} className="mx-auto mb-1 text-(--accent)" />
            <div className="text-lg font-bold text-(--text-primary)">
              {requestLog.tokens_per_second?.toFixed(1) ?? "—"}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
              Tokens/s
            </div>
          </div>
          <div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
            <Gauge size={16} className="mx-auto mb-1 text-(--accent)" />
            <div className="text-lg font-bold text-(--text-primary)">
              {totalTokens > 0 ? totalTokens.toLocaleString() : "—"}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
              Total Tokens
            </div>
          </div>
        </div>

        {/* Details Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-6">
          <DetailItem
            icon={Calendar}
            label="Timestamp"
            value={formatDateTime(requestLog.created_at)}
          />
          <DetailItem
            icon={Hash}
            label="Request Hash"
            value={requestLog.request_hash}
            mono
            truncate
          />
          <DetailItem icon={Box} label="Model" value={requestLog.model_id} />
          <DetailItem
            icon={Server}
            label="Provider"
            value={requestLog.provider_name}
          />
          <DetailItem
            icon={Key}
            label="Virtual Key"
            value={
              requestLog.virtual_key_name || requestLog.virtual_key_id || "—"
            }
          />
          <DetailItem
            icon={Tag}
            label="Request ID"
            value={requestLog.request_id}
            mono
          />
        </div>

        {/* Token Breakdown */}
        {totalTokens > 0 && (
          <div className="mb-6 p-4 rounded-lg bg-(--surface-bg) border border-(--border-subtle)">
            <h4 className="text-sm font-semibold text-(--text-primary) mb-3 flex items-center gap-2">
              <Layers size={14} className="text-(--accent)" />
              Token Usage
            </h4>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div>
                <div className="text-[11px] uppercase text-(--text-tertiary)">
                  Prompt
                </div>
                <div className="text-sm font-mono text-(--text-primary)">
                  {requestLog.tokens_prompt.toLocaleString()}
                </div>
              </div>
              <div>
                <div className="text-[11px] uppercase text-(--text-tertiary)">
                  Completion
                </div>
                <div className="text-sm font-mono text-(--text-primary)">
                  {requestLog.tokens_completion.toLocaleString()}
                </div>
              </div>
              {hasCache && (
                <>
                  <div>
                    <div className="text-[11px] uppercase text-(--text-tertiary)">
                      Cache Hit
                    </div>
                    <div className="text-sm font-mono text-green-400">
                      {requestLog.tokens_prompt_cache_hit.toLocaleString()}
                    </div>
                  </div>
                  <div>
                    <div className="text-[11px] uppercase text-(--text-tertiary)">
                      Cache Miss
                    </div>
                    <div className="text-sm font-mono text-orange-400">
                      {requestLog.tokens_prompt_cache_miss.toLocaleString()}
                    </div>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {/* Overhead Breakdown */}
        {requestLog.proxy_overhead_ms > 0 && (
          <div className="mb-6 p-4 rounded-lg bg-(--surface-bg) border border-(--border-subtle)">
            <h4 className="text-sm font-semibold text-(--text-primary) mb-3 flex items-center gap-2">
              <Gauge size={14} className="text-(--accent)" />
              Proxy Overhead Breakdown
            </h4>
            <div className="space-y-2">
              {[
                {
                  label: "Request Parsing",
                  value: requestLog.parse_ms,
                },
                {
                  label: "Model/Failover Lookup",
                  value: requestLog.model_lookup_ms,
                },
                {
                  label: "Provider Lookup",
                  value: requestLog.provider_lookup_ms,
                },
                {
                  label: "Key Decryption",
                  value: requestLog.key_decrypt_ms,
                },
              ].map(
                ({ label, value }) =>
                  value > 0 && (
                    <div key={label} className="flex justify-between text-sm">
                      <span className="text-(--text-secondary)">{label}</span>
                      <span className="font-mono text-(--text-primary)">
                        {formatDuration(value)}
                      </span>
                    </div>
                  ),
              )}
              <div className="border-t border-(--border-default) my-2" />
              <div className="flex justify-between text-sm font-semibold">
                <span className="text-(--text-primary)">Total Overhead</span>
                <span className="font-mono text-(--accent)">
                  {formatDuration(requestLog.proxy_overhead_ms)}
                </span>
              </div>
            </div>
          </div>
        )}

        {/* Error Message */}
        {requestLog.error_message && (
          <div className="p-4 rounded-lg bg-red-500/10 border border-red-500/30">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle size={14} className="text-red-400" />
              <span className="text-sm font-semibold text-red-400">Error</span>
            </div>
            <div className="text-sm text-red-300 font-mono wrap-break-words">
              {requestLog.error_message}
            </div>
          </div>
        )}
      </Modal>
    );
  }

  // App Log Detail
  const appLog = log as AppLogEntry;
  return (
    <Modal
      title="Log Entry Details"
      onClose={onClose}
      maxWidth="max-w-lg"
      scrollable
    >
      <div className="space-y-3">
        <DetailItem
          icon={Calendar}
          label="Timestamp"
          value={formatDateTime(appLog.timestamp)}
          accent
        />
        <DetailItem
          icon={Activity}
          label="Level"
          value={appLog.level.toUpperCase()}
          accent
        >
          <span
            className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
              appLog.level === "error"
                ? "bg-red-500/15 text-red-400 border border-red-500/30"
                : appLog.level === "warning"
                  ? "bg-yellow-500/15 text-yellow-400 border border-yellow-500/30"
                  : "bg-blue-500/15 text-blue-400 border border-blue-500/30"
            }`}
          >
            {appLog.level.toUpperCase()}
          </span>
        </DetailItem>
        <DetailItem icon={Tag} label="Source" value={appLog.source || "—"} />
        <DetailItem icon={FileText} label="Message" value={appLog.message}>
          <div className="relative">
            <pre className="text-sm text-(--text-primary) font-mono whitespace-pre-wrap break-all bg-(--surface-elevated) p-3 rounded-lg border border-(--border-subtle) max-h-60 overflow-y-auto">
              {appLog.message}
            </pre>
            <CopyButton text={appLog.message} />
          </div>
        </DetailItem>
      </div>
    </Modal>
  );
}
