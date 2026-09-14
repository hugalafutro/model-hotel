import type { TFunction } from "i18next";
import type { BudgetPeriod, VirtualKey } from "../../api/types";
import { formatSpend } from "../../utils/format";

/** The calendar windows a budget can run over, as the API spells them. */
export const BUDGET_PERIODS: BudgetPeriod[] = ["day", "week", "month"];

/** "$3.25 of $25.00 this month", for a key or a user that has a budget. */
export function budgetText(
	t: TFunction,
	vk: Pick<VirtualKey, "budget_usd" | "budget_period" | "budget_spent_usd">,
): string {
	if (vk.budget_usd == null) return t("budget.none");
	return t("budget.spent", {
		spent: formatSpend(vk.budget_spent_usd ?? 0),
		budget: formatSpend(vk.budget_usd),
		period: t(`budget.periodNow.${vk.budget_period ?? "month"}`),
	});
}
