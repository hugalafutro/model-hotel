import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PageErrorBoundary } from "../PageErrorBoundary";

function Broken(): never {
	throw new Error("chunk gone");
}

describe("PageErrorBoundary", () => {
	afterEach(() => vi.restoreAllMocks());

	it("renders its children when nothing throws", () => {
		render(
			<PageErrorBoundary>
				<p>page body</p>
			</PageErrorBoundary>,
		);
		expect(screen.getByText("page body")).toBeInTheDocument();
	});

	it("replaces a throwing page with an alert whose button reloads", () => {
		vi.spyOn(console, "error").mockImplementation(() => {});
		const reload = vi.fn();
		vi.spyOn(window, "location", "get").mockReturnValue({
			...window.location,
			reload,
		});
		render(
			<PageErrorBoundary>
				<Broken />
			</PageErrorBoundary>,
		);
		const alert = screen.getByRole("alert");
		expect(alert).toHaveTextContent("This page could not be loaded.");
		fireEvent.click(screen.getByRole("button", { name: "Reload page" }));
		expect(reload).toHaveBeenCalledOnce();
	});
});
