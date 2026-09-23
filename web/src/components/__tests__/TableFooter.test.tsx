import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TableFooter } from "../TableFooter";

describe("TableFooter", () => {
	it("shows the visible range of the total and nothing else while idle", () => {
		render(<TableFooter start={1} end={20} total={400} />);
		expect(screen.getByText("Showing 1–20 of 400")).toBeInTheDocument();
		expect(screen.queryByText(/Loading/)).toBeNull();
	});

	it("abbreviates large counts and keeps the exact, unformatted ones in its tooltip", () => {
		render(<TableFooter start={100201} end={100250} total={212345} />);
		const status = screen.getByText("Showing 100.2K–100.3K of 212.3K");
		expect(status).toHaveAttribute("title", "Showing 100201–100250 of 212345");
	});

	it("says nothing is shown when no rows are", () => {
		render(<TableFooter start={1} end={0} total={0} />);
		expect(screen.getByText("Showing 0 of 0")).toBeInTheDocument();
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
		expect(screen.getByText("Loading newer…")).toBeInTheDocument();
		expect(screen.getByText("Loading older…")).toBeInTheDocument();
		expect(screen.getByText("extra")).toBeInTheDocument();
	});
});
