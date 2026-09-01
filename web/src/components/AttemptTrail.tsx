import { useTranslation } from "react-i18next";
import { Layers } from "@/lib/icons";
import type { AttemptRecord } from "../api/types";
import { formatMs } from "../pages/Logs/utils";
import { DetailSectionHeader } from "./DetailSectionHeader";
import { InfoHint } from "./InfoHint";
import { StatusBadge } from "./LogDetailStatusBadge";

// breakerVerdictKey maps an attempt's breaker verdict to its label key. Unknown
// verdicts (a newer member's vocabulary) fall back to the generic key, which
// interpolates the raw word, so the row never renders an empty span.
const BREAKER_VERDICT_KEYS: Record<string, string> = {
	charge: "components.requestLogDetail.attemptBreakerCharge",
	noop: "components.requestLogDetail.attemptBreakerNoop",
	success: "components.requestLogDetail.attemptBreakerSuccess",
	alive: "components.requestLogDetail.attemptBreakerAlive",
	skipped: "components.requestLogDetail.attemptBreakerSkipped",
	disabled: "components.requestLogDetail.attemptBreakerDisabled",
};

// AttemptTrail renders the per-attempt trail of one request log row: every
// provider the request was routed to, in order, with what each one answered,
// so a "Neuralwatt 429 → Ollama 200" reads as the chain it was. Rendered by
// RequestLogDetail whenever the row carries a trail.
export function AttemptTrail({ attempts }: { attempts: AttemptRecord[] }) {
	const { t } = useTranslation();
	return (
		<div className="mb-6" data-testid="attempt-trail">
			<DetailSectionHeader icon={Layers}>
				{t("components.requestLogDetail.attemptTrail")}
				<InfoHint tooltip={t("components.requestLogDetail.attemptTrailHint")} />
			</DetailSectionHeader>
			<ol className="space-y-1">
				{attempts.map((a) => (
					<li
						key={`${a.attempt}-${a.provider_id}-${a.model}`}
						className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm p-2 ui-stat-tile"
						data-testid="attempt-trail-row"
					>
						<span className="font-mono text-xs text-(--text-tertiary) w-6 shrink-0">
							{a.attempt < 0 ? "–" : a.attempt + 1}
						</span>
						<span className="font-medium text-(--text-primary)">
							{a.provider}
						</span>
						<span className="font-mono text-xs text-(--text-secondary) truncate max-w-[14rem]">
							{a.model}
						</span>
						{a.breaker === "skipped" ? (
							<span className="ui-badge ui-badge-amber text-xs">
								{t("components.requestLogDetail.attemptSkipped")}
							</span>
						) : (
							<StatusBadge
								code={a.status ?? 0}
								state={a.status ? "completed" : "failed"}
								errorMessage={a.error_kind ?? ""}
							/>
						)}
						{a.hedged && (
							<span className="ui-badge ui-badge-purple text-xs">
								{t("components.requestLogDetail.attemptHedged")}
							</span>
						)}
						{a.breaker !== "skipped" && (
							<span className="font-mono text-xs text-(--text-tertiary)">
								{formatMs(a.duration_ms, 1)}
							</span>
						)}
						{a.error_kind && (
							<span className="font-mono text-xs text-(--text-secondary)">
								{a.error_kind}
							</span>
						)}
						{a.breaker && a.breaker !== "skipped" && (
							<span
								className="text-xs text-(--text-tertiary)"
								title={t("components.requestLogDetail.attemptBreaker", {
									verdict: a.breaker,
								})}
							>
								{t(
									BREAKER_VERDICT_KEYS[a.breaker] ??
										"components.requestLogDetail.attemptBreaker",
									{ verdict: a.breaker },
								)}
							</span>
						)}
						{a.detail && (
							<span className="basis-full font-mono text-xs text-(--text-secondary) break-words">
								{a.detail}
							</span>
						)}
					</li>
				))}
			</ol>
		</div>
	);
}
