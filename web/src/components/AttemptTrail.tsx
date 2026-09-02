import { useTranslation } from "react-i18next";
import { Layers } from "@/lib/icons";
import type { AttemptRecord } from "../api/types";
import { formatMs } from "../pages/Logs/utils";
import { DetailSectionHeader } from "./DetailSectionHeader";
import { InfoHint } from "./InfoHint";
import { StatusBadge } from "./LogDetailStatusBadge";

// Label key per breaker verdict. An unknown verdict falls back to the generic
// key, which interpolates the raw word, so the row never renders an empty span.
const BREAKER_VERDICT_KEYS: Record<string, string> = {
	charge: "components.requestLogDetail.attemptBreakerCharge",
	noop: "components.requestLogDetail.attemptBreakerNoop",
	success: "components.requestLogDetail.attemptBreakerSuccess",
	alive: "components.requestLogDetail.attemptBreakerAlive",
	skipped: "components.requestLogDetail.attemptBreakerSkipped",
	disabled: "components.requestLogDetail.attemptBreakerDisabled",
};

// AttemptTrail renders one request log row's attempt trail: every provider the
// request was routed to, in order, with what each one answered.
export function AttemptTrail({ attempts }: { attempts: AttemptRecord[] }) {
	const { t } = useTranslation();
	// Skips (attempt -1) first, then by attempt index: a hedged race reports its
	// losers in arrival order, so the winner (attempt 0) can arrive after a loser
	// (attempt 1) and would otherwise read backwards. Stable, so equal indices
	// keep their arrival order.
	const ordered = [...attempts].sort((x, y) => x.attempt - y.attempt);
	return (
		<div className="mb-6" data-testid="attempt-trail">
			<DetailSectionHeader icon={Layers}>
				{t("components.requestLogDetail.attemptTrail")}
				<InfoHint tooltip={t("components.requestLogDetail.attemptTrailHint")} />
			</DetailSectionHeader>
			<ol className="space-y-1">
				{ordered.map((a) => (
					<li
						key={`${a.attempt}-${a.provider_id}-${a.model}-${a.status ?? 0}-${a.duration_ms}`}
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
						) : a.error_kind === "hedge_superseded" ? (
							// Abandoned because another candidate won: not a failure,
							// the client was served, so the badge is neutral.
							<span
								className="ui-badge ui-badge-neutral text-xs"
								data-testid="attempt-superseded"
							>
								{t("components.requestLogDetail.attemptSuperseded")}
							</span>
						) : a.status ? (
							<StatusBadge code={a.status} state="completed" errorMessage="" />
						) : (
							<span className="ui-badge ui-badge-red text-xs">
								{t("components.requestLogDetail.attemptNoStatus")}
							</span>
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
