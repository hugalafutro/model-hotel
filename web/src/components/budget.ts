import type { TFunction } from "i18next";
import type { BudgetPeriod, VirtualKey } from "../api/types";
import { formatSpend, numOrNull } from "../utils/format";

/** The calendar windows a budget can run over, as the API spells them. */
export const BUDGET_PERIODS: BudgetPeriod[] = ["day", "week", "month"];

/** The budget half of a key or user payload; no amount means no budget. */
export function budgetPayload(
	amount: string,
	period: BudgetPeriod,
): { budget_usd: number | null; budget_period: BudgetPeriod | null } {
	return {
		budget_usd: numOrNull(amount),
		budget_period: amount !== "" ? period : null,
	};
}

/** "$3.25 of $25.00 this month", for a key or a user that has a budget. */
export function budgetText(
	t: TFunction,
	vk: Pick<VirtualKey, "budget_usd" | "budget_period" | "budget_spent_usd">,
): string {
	if (vk.budget_usd == null) return t("budget.none");
	if (vk.budget_spent_usd == null) {
		// The member could not sum the period yet (its store did not answer);
		// the proxy refuses the subject's requests until it can.
		return t("budget.spentUnknown", {
			budget: formatSpend(vk.budget_usd),
			period: t(`budget.periodNow.${vk.budget_period ?? "month"}`),
		});
	}
	return t("budget.spent", {
		spent: formatSpend(vk.budget_spent_usd),
		budget: formatSpend(vk.budget_usd),
		period: t(`budget.periodNow.${vk.budget_period ?? "month"}`),
	});
}
