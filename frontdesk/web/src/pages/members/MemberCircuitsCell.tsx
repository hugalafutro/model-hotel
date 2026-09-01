import { useTranslation } from "react-i18next";
import type { MemberStatus } from "../../api/types";

// The Members table's circuits column: how many circuits this member's
// breaker holds open or owes a probe, with the ledger itself on hover
// (provider, model, state, cause, next retry). Which member is dark for which
// model, without a terminal per member. Read-only; the fleet reset button is
// the write side.
export function MemberCircuitsCell({
	status,
	hasToken,
}: {
	status: MemberStatus;
	hasToken: boolean;
}) {
	const { t } = useTranslation();
	const ledger = status.circuits;
	if (!ledger) {
		return (
			<span className="fd-faint" data-testid="member-circuits-unknown">
				{hasToken ? t("members.circuitsUnknown") : t("members.noToken")}
			</span>
		);
	}
	if (ledger.open.length === 0) {
		return (
			<span className="fd-faint" data-testid="member-circuits-none">
				{t("members.circuitsNone")}
			</span>
		);
	}
	const lines = ledger.open.map((c) => {
		const parts = [
			`${c.provider ?? c.provider_id} / ${c.model}`,
			c.state === "half-open"
				? t("members.circuitStateHalfOpen")
				: c.quota_pinned
					? t("members.circuitStatePinned")
					: t("members.circuitStateOpen"),
		];
		if (c.cause) parts.push(c.cause);
		if (c.next_retry_at) {
			parts.push(
				t("members.circuitRetry", {
					when: new Date(c.next_retry_at).toLocaleTimeString(),
				}),
			);
		}
		return parts.join(" · ");
	});
	return (
		<span
			className="ui-badge ui-badge-warn"
			data-testid="member-circuits-open"
			title={lines.join("\n")}
		>
			{t("members.circuitsOpen", { count: ledger.open.length })}
		</span>
	);
}
