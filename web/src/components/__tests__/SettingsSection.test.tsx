import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Settings } from "lucide-react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { SettingsSection } from "../SettingsSection";

describe("SettingsSection", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders title text", () => {
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed={false}
				onToggle={onToggle}
			>
				<div>Child content</div>
			</SettingsSection>,
		);
		expect(screen.getByText("Test Settings")).toBeInTheDocument();
	});

	it("renders icon", () => {
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed={false}
				onToggle={onToggle}
			>
				<div>Child content</div>
			</SettingsSection>,
		);
		// Icon renders as an SVG with class containing "lucide-settings"
		const icon = document.querySelector(".lucide-settings");
		expect(icon).toBeInTheDocument();
	});

	it("renders children when not collapsed", () => {
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed={false}
				onToggle={onToggle}
			>
				<div data-testid="child">Child content</div>
			</SettingsSection>,
		);
		expect(screen.getByTestId("child")).toBeInTheDocument();
	});

	it("hides children when collapsed (grid-rows-[0fr] class)", () => {
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed
				onToggle={onToggle}
			>
				<div data-testid="child">Child content</div>
			</SettingsSection>,
		);
		// The grid container is the parent of overflow-hidden div
		const overflowDiv = screen.getByTestId("child").parentElement;
		const gridContainer = overflowDiv?.parentElement;
		expect(gridContainer).toHaveClass("grid-rows-[0fr]");
	});

	it("calls onToggle when toggle button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed={false}
				onToggle={onToggle}
			>
				<div>Child content</div>
			</SettingsSection>,
		);
		await user.click(screen.getByRole("button"));
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("renders custom children content", () => {
		renderWithProviders(
			<SettingsSection
				icon={Settings}
				title="Test Settings"
				collapsed={false}
				onToggle={onToggle}
			>
				<p>Custom paragraph content</p>
				<button type="button">Action Button</button>
			</SettingsSection>,
		);
		expect(screen.getByText("Custom paragraph content")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Action Button" }),
		).toBeInTheDocument();
	});
});
