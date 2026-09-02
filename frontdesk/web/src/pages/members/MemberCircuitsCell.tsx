import { useTranslation } from "react-i18next";
import type { MemberStatus } from "../../api/types";
import { formatAbsolute } from "../../utils/time";

// The Members table's circuits column: how many circuits this member's breaker
// holds open or owes a probe, with the ledger itself on hover (provider, model,
// state, cause, next retry). Read-only; the fleet reset button is the write
// side.
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
	if (ledger.total === 0) {
		return (
			<span className="fd-faint" data-testid="member-circuits-none">
				{t("members.circuitsNone")}
			</span>
		);
	}
	const stateLabel = (c: (typeof ledger.open)[number]) => {
		if (c.state === "half-open") return t("members.circuitStateHalfOpen");
		if (c.state !== "open") return c.state;
		return c.quota_pinned
			? t("members.circuitStatePinned")
			: t("members.circuitStateOpen");
	};
	const lines = ledger.open.map((c) => {
		const parts = [
			`${c.provider ?? c.provider_id.slice(0, 8)} / ${c.model}`,
			stateLabel(c),
		];
		if (c.cause) parts.push(c.cause);
		if (c.next_retry_at) {
			// Date and time in the UI's language: a quota pin can reach a day out.
			parts.push(
				t("members.circuitRetry", { when: formatAbsolute(c.next_retry_at) }),
			);
		}
		return parts.join(" · ");
	});
	if (ledger.total > ledger.open.length) {
		lines.push(
			t("members.circuitsShowing", {
				shown: ledger.open.length,
				total: ledger.total,
			}),
		);
	}
	return (
		<span
			className="ui-badge ui-badge-warn"
			data-testid="member-circuits-open"
			title={lines.join("\n")}
		>
			{t("members.circuitsOpen", { count: ledger.total })}
		</span>
	);
}
