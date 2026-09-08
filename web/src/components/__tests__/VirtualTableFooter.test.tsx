import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { VirtualTableFooter } from "../VirtualTableFooter";

describe("VirtualTableFooter", () => {
	it("shows the range and nothing else while idle", () => {
		render(
			<VirtualTableFooter
				range="1–20 / 400"
				isLoadingBefore={false}
				isLoadingAfter={false}
			/>,
		);
		expect(screen.getByText("1–20 / 400")).toBeInTheDocument();
		expect(screen.queryByText(/Loading/)).toBeNull();
	});

	it("names each direction that is loading and renders extra status", () => {
		render(
			<VirtualTableFooter range="1–20 / 400" isLoadingBefore isLoadingAfter>
				<span>extra</span>
			</VirtualTableFooter>,
		);
		expect(screen.getByText("Loading newer…")).toBeInTheDocument();
		expect(screen.getByText("Loading older…")).toBeInTheDocument();
		expect(screen.getByText("extra")).toBeInTheDocument();
	});
});
