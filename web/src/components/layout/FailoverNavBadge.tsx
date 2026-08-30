import { useTranslation } from "react-i18next";
import type { CircuitBreakerStatus } from "../../api/types";

/** Renders a count that a truncated payload left missing as 0, never as blank. */
function count(n: number | undefined): number {
	return Number.isFinite(n) ? (n as number) : 0;
}

/**
 * The two provider counts beside the Failover item, with a tooltip that always
 * explains what they mean (they track providers, not groups, a common mix-up)
 * and names the unhealthy providers, split by what the breaker is actually
 * doing with each: skipping it entirely, waiting on its quota reset, or keeping
 * it in rotation with only some models blocked.
 *
 * The red number is the providers the breaker is skipping, which is the derived
 * verdict and not the endpoint's `open` tally. That tally counts providers whose
 * most degraded model circuit is open, and since circuits are keyed per model
 * that includes providers still serving every other model: at the default span
 * of 2 the first model to go dark leaves the provider in rotation. Those
 * providers stay visible in the amber count, which is therefore "unhealthy but
 * still routing" rather than only "recovering". The two derived numbers add up
 * to the endpoint's half_open + open, so nothing is dropped, only reclassified.
 */
export function FailoverNavBadge({
	cbStatus,
	navSep,
}: {
	cbStatus: CircuitBreakerStatus;
	navSep: string;
}) {
	const { t } = useTranslation();
	const unhealthy = cbStatus.providers?.filter(
		(p) => p.state === "open" || p.state === "half-open",
	);
	// Without the detail rows there is no verdict to read, only the endpoint's
	// own tally. Reporting it unchanged is the truthful fallback: it is the whole
	// of what that response says. The nav badge always asks for detail, so this
	// is the shape of the plain list endpoint rather than a state it reaches.
	const skippedCount = unhealthy
		? unhealthy.filter((p) => p.provider_open).length
		: count(cbStatus.open);
	const routingCount = unhealthy
		? unhealthy.length - skippedCount
		: count(cbStatus.half_open);

	const explain = t("layout.nav.failoverBadgeExplain", {
		closed: count(cbStatus.closed),
		halfOpen: routingCount,
		open: skippedCount,
	});
	let title = explain;
	if (unhealthy && unhealthy.length > 0) {
		// Quota-pinned circuits wait until the provider's quota window resets,
		// which can be days. Listing them beside ordinary sixty-second cooldowns
		// reads as "back shortly", so they get their own line.
		//
		// The state check is load-bearing: quota_pinned stays set for the whole
		// life of the circuit, and a pinned circuit whose deadline has passed is
		// reported as half-open (ready to probe) with no next_retry_at. Bucketing
		// on the flag alone would keep claiming a provider is waiting on a quota
		// window that has already reset. This mirrors the per-entry rule, where
		// cooldown-over wins over the quota tooltip.
		const stillPinned = (p: (typeof unhealthy)[number]) =>
			Boolean(p.quota_pinned) && p.state === "open";
		// Circuits are keyed per model, so a provider with an open circuit is not
		// necessarily a provider that is down: at the default span of 2 the first
		// model to go dark leaves every other model of that provider serving. Only
		// the derived verdict says the breaker is skipping the provider outright,
		// and the two buckets get separate lines so a partial outage is never read
		// as a dead provider. Each name carries the models it is blocking, since
		// that is what tells an operator which of the two this is.
		const names = (list: typeof unhealthy) =>
			list
				.map((p) => {
					const name = p.provider_name || p.provider_id;
					const models = p.open_models;
					return models && models.length > 0
						? t("layout.nav.failoverBadgeOpenModels", {
								provider: name,
								models: models.join(", "),
							})
						: name;
				})
				.join(", ");
		const pinned = unhealthy.filter(stillPinned);
		const skipped = unhealthy.filter((p) => !stillPinned(p) && p.provider_open);
		const partial = unhealthy.filter(
			(p) => !stillPinned(p) && !p.provider_open,
		);
		const lines = [explain];
		if (skipped.length > 0) {
			lines.push(
				t("layout.nav.failoverBadgeSkippedTooltip", {
					count: skipped.length,
					providers: names(skipped),
				}),
			);
		}
		if (partial.length > 0) {
			lines.push(
				t("layout.nav.failoverBadgeTooltip", {
					count: partial.length,
					providers: names(partial),
				}),
			);
		}
		if (pinned.length > 0) {
			lines.push(
				t("layout.nav.failoverBadgeQuotaTooltip", {
					count: pinned.length,
					providers: names(pinned),
				}),
			);
		}
		title = lines.join("\n");
	}
	return (
		<span
			className="inline-flex items-center gap-[2px] leading-[1.6] translate-y-[1px] ui-badge ui-badge-neutral"
			title={title}
		>
			<span
				className="text-amber-400 badge-text"
				data-testid="failover-badge-routing"
			>
				{routingCount}
			</span>
			<span className="text-(--text-secondary)">{navSep}</span>
			<span
				className="text-red-400 badge-text"
				data-testid="failover-badge-skipped"
			>
				{skippedCount}
			</span>
		</span>
	);
}
