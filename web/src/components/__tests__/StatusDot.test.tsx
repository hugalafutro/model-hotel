import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusDot } from "../StatusDot";

describe("StatusDot", () => {
	it("spins instead of showing a colour while the check runs", () => {
		const { container } = render(
			<StatusDot state="checking" label="Checking…" />,
		);
		expect(screen.getByText("Checking…")).toBeInTheDocument();
		expect(container.querySelector(".animate-spin")).not.toBeNull();
		expect(container.querySelector(".rounded-full")).toBeNull();
	});

	it.each([
		["ok", "bg-green-500"],
		["warn", "bg-amber-500"],
		["error", "bg-red-500"],
	] as const)("colours the %s dot", (state, cls) => {
		const { container } = render(<StatusDot state={state} label="Status" />);
		expect(container.querySelector(".rounded-full")?.className).toContain(cls);
	});

	it("hides the dot from the accessibility tree", () => {
		const { container } = render(<StatusDot state="ok" label="Configured" />);
		expect(container.querySelector(".rounded-full")).toHaveAttribute(
			"aria-hidden",
			"true",
		);
	});
});
