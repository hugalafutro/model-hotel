import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import i18n from "../../i18n";
import { formatCompact, formatNumber } from "../../utils/format";
import { TableFooter } from "../TableFooter";

const range = (start: string, end: string, total: string) =>
	i18n.t("common.showingRange", { start, end, total });

describe("TableFooter", () => {
	it("shows the visible range of the total and nothing else while idle", () => {
		render(<TableFooter start={1} end={20} total={400} />);
		expect(screen.getByText(range("1", "20", "400"))).toBeInTheDocument();
		expect(screen.queryByText(i18n.t("common.loadingNewer"))).toBeNull();
		expect(screen.queryByText(i18n.t("common.loadingOlder"))).toBeNull();
	});

	it("Regression pin: keeps the row range exact and locale-grouped, compacts only the total, and puts the grouped total in its tooltip", () => {
		render(<TableFooter start={100201} end={100245} total={212345} />);
		const status = screen.getByText(
			range(formatNumber(100201), formatNumber(100245), formatCompact(212345)),
		);
		expect(status).toHaveAttribute(
			"title",
			range(formatNumber(100201), formatNumber(100245), formatNumber(212345)),
		);
	});

	it("says nothing is shown when no rows are", () => {
		render(<TableFooter start={1} end={0} total={0} />);
		expect(
			screen.getByText(i18n.t("common.showingNone", { total: "0" })),
		).toBeInTheDocument();
	});

	it("names each direction that is loading and renders extra status", () => {
		render(
			<TableFooter
				start={1}
				end={20}
				total={400}
				isLoadingBefore
				isLoadingAfter
			>
				<span>extra</span>
			</TableFooter>,
		);
		expect(screen.getByText(i18n.t("common.loadingNewer"))).toBeInTheDocument();
		expect(screen.getByText(i18n.t("common.loadingOlder"))).toBeInTheDocument();
		expect(screen.getByText("extra")).toBeInTheDocument();
	});
});
