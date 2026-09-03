import { useTranslation } from "react-i18next";
import {
	Layers,
	Shield,
	ShieldAlert,
	ShieldCheck,
	ShieldOff,
} from "@/lib/icons";
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

// collapseWhitespace renders text the way the backend stores a trail detail
// (runs of whitespace folded to one space), so a stored detail can be looked
// for inside the row's error_message.
function collapseWhitespace(message: string): string {
	return message.split(/\s+/).filter(Boolean).join(" ");
}

// Icon per breaker verdict, the shield family the Failover page draws the
// circuit with: a charge is the alert, a credit the check, disabled the
// struck shield, and the neutral verdicts (alive, untouched) the plain one.
const BREAKER_VERDICT_ICONS: Record<
	string,
	React.ComponentType<{ size?: number; className?: string }>
> = {
	charge: ShieldAlert,
	success: ShieldCheck,
	disabled: ShieldOff,
};

// AttemptTrail renders one request log row's attempt trail: every provider the
// request was routed to, in order, with what each one answered. errorMessage
// is the row's own error: the last attempt's detail is left out when that
// error already carries it (as a whole, or quoted inside the terminal
// message), since the error block below the trail renders it once already.
export function AttemptTrail({
	attempts,
	errorMessage,
}: {
	attempts: AttemptRecord[];
	errorMessage?: string;
}) {
	const { t } = useTranslation();
	// Skips (attempt -1) first, then by attempt index: a hedged race reports its
	// losers in arrival order, so the winner (attempt 0) can arrive after a loser
	// (attempt 1) and would otherwise read backwards. Stable, so equal indices
	// keep their arrival order.
	const ordered = [...attempts].sort((x, y) => x.attempt - y.attempt);
	const terminalMessage = errorMessage ? collapseWhitespace(errorMessage) : "";
	const last = ordered[ordered.length - 1];
	// The terminal message is the attempt's error text or quotes it, at the
	// end ("failed on attempt 1: ...") or mid-sentence ("returned HTTP 503 on
	// attempt 1"), so the detail is looked for as a run inside it. A detail
	// the backend capped ends in an ellipsis the message does not have.
	const repeatsError = (a: AttemptRecord) =>
		a === last &&
		!!a.detail &&
		terminalMessage.includes(a.detail.replace(/…$/, ""));
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
						{(a.error_kind || (a.breaker && a.breaker !== "skipped")) && (
							// The verdicts get their own line under the provider, status
							// and timing, rather than wrapping wherever the width runs out;
							// indented past the number column so they line up with the
							// provider name.
							<span className="basis-full flex items-baseline gap-x-2 pl-8">
								{a.error_kind && (
									<span className="font-mono text-xs text-(--text-secondary)">
										{a.error_kind}
									</span>
								)}
								{a.breaker && a.breaker !== "skipped" && (
									// The shield marks the verdict as the breaker's, not another
									// word of the error kind or the timing beside it.
									<span
										className="inline-flex items-center gap-1 text-xs text-(--text-tertiary)"
										title={t("components.requestLogDetail.attemptBreaker", {
											verdict: a.breaker,
										})}
									>
										{(() => {
											const Icon = BREAKER_VERDICT_ICONS[a.breaker] ?? Shield;
											return <Icon size={11} aria-hidden="true" />;
										})()}
										{t(
											BREAKER_VERDICT_KEYS[a.breaker] ??
												"components.requestLogDetail.attemptBreaker",
											{ verdict: a.breaker },
										)}
									</span>
								)}
							</span>
						)}
						{a.detail && !repeatsError(a) && (
							<span className="basis-full pl-8 font-mono text-xs text-(--text-secondary) break-words">
								{a.detail}
							</span>
						)}
					</li>
				))}
			</ol>
		</div>
	);
}
