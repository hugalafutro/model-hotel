import { useTranslation } from "react-i18next";
import type {
	CircuitBreakerProviderStatus,
	CircuitBreakerStatus,
} from "../../api/types";

/** Renders a count that a truncated payload left missing as 0, never as blank. */
function count(n: number | undefined): number {
	return Number.isFinite(n) ? (n as number) : 0;
}

/**
 * The two provider counts beside the Failover item, with a tooltip that says
 * what they mean (they track providers, not groups) and names the unhealthy
 * providers split by what the breaker does with each: skipping it entirely,
 * waiting on its quota reset, or keeping it in rotation with only some models
 * blocked.
 *
 * The red number is the derived provider_open verdict, not the endpoint's
 * `open` tally: that tally counts providers whose most degraded model circuit
 * is open, which includes providers still serving every other model. Those sit
 * in the amber count, which is therefore "unhealthy but still routing" rather
 * than only "recovering". The two numbers add up to half_open + open.
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
	// Without the detail rows there is no verdict to read, so the endpoint's own
	// tally is reported unchanged. The nav badge always asks for detail, so this
	// is the shape of the plain list endpoint.
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
		// which can be days, so they get their own line rather than reading as
		// "back shortly" beside ordinary sixty-second cooldowns.
		//
		// The state check is load-bearing: quota_pinned stays set for the whole
		// life of the circuit, and a pinned circuit whose deadline has passed is
		// reported as half-open (ready to probe) with no next_retry_at. Bucketing
		// on the flag alone would keep claiming a provider is waiting on a quota
		// window that has already reset.
		const stillPinned = (p: (typeof unhealthy)[number]) =>
			Boolean(p.quota_pinned) && p.state === "open";
		// Names one bucket's providers, each with the models it is blocking.
		// Circuits are keyed per model, so an open circuit is not necessarily a
		// dead provider: at the default span of 2 the first model to go dark
		// leaves every other model of that provider serving. Only the derived
		// verdict says the breaker skips the provider outright, so the buckets get
		// separate lines and a partial outage is never read as a dead provider.
		//
		// A quota-pinned provider the breaker skips outright lists no models: its
		// quota window is spent for every model it serves. Pins land per circuit,
		// so a pinned provider still routing the rest is a partial outage and
		// keeps naming the models that are dark.
		const names = (
			list: typeof unhealthy,
			withModels: (p: CircuitBreakerProviderStatus) => boolean = () => true,
		) =>
			list
				.map((p) => {
					const name = p.provider_name || p.provider_id;
					const models = withModels(p) ? p.open_models : undefined;
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
					providers: names(pinned, (p) => !p.provider_open),
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
