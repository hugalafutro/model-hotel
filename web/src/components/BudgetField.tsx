import { useTranslation } from "react-i18next";
import type { BudgetPeriod } from "../api/types";
import { BUDGET_PERIODS } from "./budget";

/** The bounds the amount input accepts, matching the API's. */
const AMOUNT = { min: "0.01", max: "10000000", step: "0.01" } as const;

/**
 * A dollar budget with its calendar period. An empty amount means no budget,
 * which is what the placeholder says; the period only means something next
 * to an amount, so it is inert until one is typed.
 */
export function BudgetField({
	idPrefix,
	amount,
	period,
	onAmountChange,
	onPeriodChange,
	disabled = false,
	labelClassName = "block text-sm font-medium text-gray-300 mb-1",
}: {
	idPrefix: string;
	amount: string;
	period: BudgetPeriod;
	onAmountChange: (value: string) => void;
	onPeriodChange: (value: BudgetPeriod) => void;
	disabled?: boolean;
	labelClassName?: string;
}) {
	const { t } = useTranslation();
	return (
		<div className="grid grid-cols-2 gap-4">
			<div>
				<label htmlFor={`${idPrefix}-budget`} className={labelClassName}>
					{t("budget.amount")}
				</label>
				<input
					id={`${idPrefix}-budget`}
					type="number"
					{...AMOUNT}
					value={amount}
					onChange={(e) => onAmountChange(e.target.value)}
					className="ui-input"
					placeholder={t("budget.none")}
					disabled={disabled}
					data-testid={`${idPrefix}-budget`}
				/>
			</div>
			<div>
				<label htmlFor={`${idPrefix}-budget-period`} className={labelClassName}>
					{t("budget.period")}
				</label>
				<select
					id={`${idPrefix}-budget-period`}
					value={period}
					onChange={(e) => onPeriodChange(e.target.value as BudgetPeriod)}
					className="ui-input"
					disabled={disabled || amount === ""}
					data-testid={`${idPrefix}-budget-period`}
				>
					{BUDGET_PERIODS.map((p) => (
						<option key={p} value={p}>
							{t(`budget.periods.${p}`)}
						</option>
					))}
				</select>
			</div>
			<p className="col-span-2 text-xs text-gray-500 -mt-2">
				{t("budget.hint")}
			</p>
		</div>
	);
}
