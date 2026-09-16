import { describe, expect, it } from "vitest";
import { budgetText } from "../budget";

// A fake translator that renders the key with its values, so the assertion
// is about which key and which values, not about English copy.
const t = ((key: string, opts?: Record<string, unknown>) =>
	opts ? `${key} ${JSON.stringify(opts)}` : key) as Parameters<
	typeof budgetText
>[0];

describe("budgetText", () => {
	it("says no budget without one", () => {
		expect(budgetText(t, { budget_usd: null })).toBe("budget.none");
	});

	it("shows the spend against the budget for the period", () => {
		expect(
			budgetText(t, {
				budget_usd: 25,
				budget_period: "month",
				budget_spent_usd: 3.25,
			}),
		).toBe(
			'budget.spent {"spent":"$3.25","budget":"$25.00","period":"budget.periodNow.month"}',
		);
	});

	it("does not read an unsummed period as nothing spent", () => {
		expect(budgetText(t, { budget_usd: 25, budget_period: "day" })).toBe(
			'budget.spentUnknown {"budget":"$25.00","period":"budget.periodNow.day"}',
		);
	});
});
