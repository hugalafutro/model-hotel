import { useTranslation } from "react-i18next";
import type { GroupClaim } from "../../api/types";
import {
	CategoryGroup,
	DetailRow,
} from "../../pages/Providers/discoveryPrimitives";
import { formatDateTimeShort } from "../../utils/format";

/**
 * Failover groups discovery disabled: `hotel/<model>` routing for them is
 * dead. No Retest (a retest is provider-scoped and a group is not) and no
 * dismiss (the claim clears itself when the group is routable again).
 */
export function GroupClaimsSection({
	groupClaims,
}: {
	groupClaims: GroupClaim[];
}) {
	const { t } = useTranslation();
	if (groupClaims.length === 0) return null;
	return (
		<CategoryGroup
			sign="⊘"
			count={groupClaims.length}
			badgeVariant="ui-badge-orange"
			label={t("providers.discrepancies.groupClaims")}
			testId="discrepancy-group-claims"
		>
			<div className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
				{groupClaims.map((g) => (
					<div
						key={g.display_model}
						data-testid="discrepancy-group-claim"
						data-display-model={g.display_model}
					>
						<DetailRow
							stacked
							primary={g.display_model}
							secondary={
								<>
									{t("providers.discrepancies.groupRoutable", {
										routable: g.routable_count,
										members: g.member_count,
									})}
									{" · "}
									{t("providers.discrepancies.groupDisabledAt", {
										when: formatDateTimeShort(g.disabled_at),
									})}
								</>
							}
						/>
					</div>
				))}
			</div>
		</CategoryGroup>
	);
}
