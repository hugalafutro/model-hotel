import { useTranslation } from "react-i18next";
import {
	AlertTriangle,
	Gauge,
	Layers,
	Shield,
	ShieldAlert,
	ShieldCheck,
	ShieldOff,
	Timer,
	Unplug,
} from "@/lib/icons";
import type { AttemptRecord } from "../api/types";
import { formatMs } from "../utils/logHelpers";
import { DetailSectionHeader } from "./DetailSectionHeader";
import { InfoHint } from "./InfoHint";
import { StatusBadge, statusBadgeLabel } from "./LogDetailStatusBadge";

// Label key per breaker verdict: the word after "breaker:", so every verdict
// names the breaker and none reads as spend ("credited", "charged"). An unknown
// verdict falls back to the raw word, so the row never renders an empty span.
const BREAKER_VERDICT_KEYS: Record<string, string> = {
	charge: "components.requestLogDetail.attemptBreakerCharge",
	noop: "components.requestLogDetail.attemptBreakerNoop",
	success: "components.requestLogDetail.attemptBreakerSuccess",
	alive: "components.requestLogDetail.attemptBreakerAlive",
	skipped: "components.requestLogDetail.attemptBreakerSkipped",
	disabled: "components.requestLogDetail.attemptBreakerDisabled",
};

// Label key per error kind (internal/proxy/reqerror.go). hedge_superseded is
// absent on purpose: its badge says it. An unknown kind renders the raw word.
const ERROR_KIND_KEYS: Record<string, string> = {
	client_disconnect: "components.requestLogDetail.attemptKindClientDisconnect",
	provider_error: "components.requestLogDetail.attemptKindProviderError",
	provider_model_gone:
		"components.requestLogDetail.attemptKindProviderModelGone",
	provider_not_entitled:
		"components.requestLogDetail.attemptKindProviderNotEntitled",
	provider_bad_request:
		"components.requestLogDetail.attemptKindProviderBadRequest",
	provider_saturated:
		"components.requestLogDetail.attemptKindProviderSaturated",
	provider_quota_exhausted:
		"components.requestLogDetail.attemptKindProviderQuotaExhausted",
	provider_timeout: "components.requestLogDetail.attemptKindProviderTimeout",
	failover_timeout: "components.requestLogDetail.attemptKindFailoverTimeout",
	retry_timeout: "components.requestLogDetail.attemptKindRetryTimeout",
	internal: "components.requestLogDetail.attemptKindInternal",
	validation: "components.requestLogDetail.attemptKindValidation",
	auth: "components.requestLogDetail.attemptKindAuth",
};

// Icon per error kind: the hangup, the clock and the gauge for the kinds a
// reader can act on differently; every other kind is a plain warning.
const ERROR_KIND_ICONS: Record<
	string,
	React.ComponentType<{ size?: number; className?: string }>
> = {
	client_disconnect: Unplug,
	provider_timeout: Timer,
	failover_timeout: Timer,
	retry_timeout: Timer,
	provider_saturated: Gauge,
};

/** The breaker refused this candidate before the request left. */
function isSkipped(a: AttemptRecord): boolean {
	return a.breaker === "skipped";
}

// collapseWhitespace renders text the way the backend stores a trail detail
// (runs of whitespace folded to one space), so a stored detail can be looked
// for inside the row's error_message.
function collapseWhitespace(message: string): string {
	return message.split(/\s+/).filter(Boolean).join(" ");
}

// The gateway's own fixed sentences (internal/proxy/hedging.go), one per kind
// it stamps itself: each says exactly what the SUPERSEDED badge or the kind
// label on the same row says, so the row never prints both.
const GATEWAY_FIXED_DETAILS = new Set([
	"superseded by the winner while in flight",
	"client disconnected while in flight",
]);

// Error kinds the badge on the same row already states: the SUPERSEDED badge
// is hedge_superseded, and an HTTP error status badge is the provider erroring.
// A status below 400 does not say it: a stream that opened 200 and failed
// mid-stream keeps the upstream 200 on its record, so the kind is the only
// mark of the failure on that row.
function kindSaysMore(a: AttemptRecord): boolean {
	if (!a.error_kind) return false;
	if (a.error_kind === "hedge_superseded") return false;
	return !(a.error_kind === "provider_error" && !!a.status && a.status >= 400);
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
	// Whether the detail carries anything the rest of the row does not say
	// already. It is dropped when: the row's own error message below the trail
	// is that same text (the terminal message quotes the attempt's error text
	// at the end, "failed on attempt 1: ...", or mid-sentence, "returned HTTP
	// 503 on attempt 1", so it is looked for as a run inside it, and a detail
	// the backend capped ends in an ellipsis the message does not have); it
	// only restates the status the badge shows; or it is one of the gateway's
	// fixed sentences, which the badge or the kind label shows too.
	const detailSaysMore = (a: AttemptRecord) => {
		const detail = a.detail?.trim();
		if (!detail) return false;
		if (a === last && terminalMessage.includes(detail.replace(/…$/, ""))) {
			return false;
		}
		if (/^HTTP \d{3}$/.test(detail)) return false;
		if (GATEWAY_FIXED_DETAILS.has(detail)) return false;
		return a.status ? detail !== statusBadgeLabel(a.status, t) : true;
	};
	// A skip IS the breaker's verdict: it refused the candidate before the
	// request left. The SKIPPED badge on the first line says that already, so
	// the row does not repeat it as a second-line verdict.
	const showsVerdict = (
		a: AttemptRecord,
	): a is AttemptRecord & { breaker: string } =>
		Boolean(a.breaker) && !isSkipped(a);
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
						className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm p-2 ui-stat-tile"
						data-testid="attempt-trail-row"
					>
						<span className="font-mono text-xs text-(--text-tertiary) w-6 shrink-0">
							{a.attempt < 0 ? "–" : a.attempt + 1}
						</span>
						{/* Provider and model each get their own fixed column, truncated
						    separately, so the badges after them start at the same x on
						    every row however long either name is. */}
						<span
							className="font-medium text-(--text-primary) w-36 shrink-0 truncate"
							title={a.provider}
						>
							{a.provider}
						</span>
						<span
							className="font-mono text-xs text-(--text-secondary) w-44 shrink-0 truncate"
							title={a.model}
						>
							{a.model}
						</span>
						{/* Verdict badges and the timing travel as one non-wrapping
						    cluster: when the row runs out of width the whole cluster
						    drops to the next line together, never the timing alone. */}
						<span className="flex items-center gap-x-2 whitespace-nowrap">
							{isSkipped(a) ? (
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
								<StatusBadge
									code={a.status}
									state="completed"
									errorMessage=""
									compact
								/>
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
							{!isSkipped(a) && (
								<span className="font-mono text-xs text-(--text-tertiary)">
									{formatMs(a.duration_ms, 1)}
								</span>
							)}
						</span>
						{(showsVerdict(a) || kindSaysMore(a) || detailSaysMore(a)) && (
							// A second line carrying the breaker verdict and whatever the
							// first line does not already say, indented past the number
							// column so it lines up with the provider name. The verdict
							// lives here rather than beside the timing because a row that
							// runs long wraps it to its own line anyway, and a wrapped
							// flex child starts at the row's start edge, under the number.
							<span
								className="basis-full flex flex-wrap items-baseline gap-x-2 ps-8"
								data-testid="attempt-trail-meta"
							>
								{showsVerdict(a) && (
									// The shield marks the verdict as the breaker's, not
									// another word of the timing above it.
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
										{t("components.requestLogDetail.attemptBreaker", {
											verdict: BREAKER_VERDICT_KEYS[a.breaker]
												? t(BREAKER_VERDICT_KEYS[a.breaker])
												: a.breaker,
										})}
									</span>
								)}
								{kindSaysMore(a) && a.error_kind && (
									// The kind wears the same dress as the verdict, an icon
									// and a plain-text label, so it reads as a classification
									// and not as a twin of the mono detail beside it. The raw
									// kind stays in the tooltip for anyone grepping the logs.
									<span
										className="inline-flex items-center gap-1 text-xs text-(--text-tertiary)"
										title={a.error_kind}
										data-testid="attempt-kind"
									>
										{(() => {
											const Icon =
												ERROR_KIND_ICONS[a.error_kind] ?? AlertTriangle;
											return <Icon size={11} aria-hidden="true" />;
										})()}
										{ERROR_KIND_KEYS[a.error_kind]
											? t(ERROR_KIND_KEYS[a.error_kind])
											: a.error_kind}
									</span>
								)}
								{detailSaysMore(a) && (
									<span className="font-mono text-xs text-(--text-secondary) break-words">
										{a.detail}
									</span>
								)}
							</span>
						)}
					</li>
				))}
			</ol>
		</div>
	);
}
