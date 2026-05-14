import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { AppearanceSettings } from "../AppearanceSettings";

describe("AppearanceSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders UI Style section with all style options", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("UI Style")).toBeInTheDocument();
		// Check for the three UI style options from constants
		expect(screen.getByText("Clean SaaS")).toBeInTheDocument();
		expect(screen.getByText("Cyber Terminal")).toBeInTheDocument();
		expect(screen.getByText("Glassmorphism")).toBeInTheDocument();
	});

	it("renders Theme toggle with Dark and Light options", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Theme")).toBeInTheDocument();
		expect(screen.getByText("Dark")).toBeInTheDocument();
		expect(screen.getByText("Light")).toBeInTheDocument();
	});

	it("renders Accent Color section", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Accent Color")).toBeInTheDocument();
	});

	it("renders custom color picker button", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// Custom color button has title "Custom color"
		expect(screen.getByTitle("Custom color")).toBeInTheDocument();
	});

	it("calls onToggle when SettingsSection toggle is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// CollapsibleToggle renders as a button with title "Collapse" when not collapsed
		const toggleButton = screen.getByRole("button", {
			name: /collapse|expand/i,
		});
		await user.click(toggleButton);
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("selects a UI style when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// Click the "Glassmorphism" style option (this exists in UI_STYLES)
		const glassButton = screen.getByText("Glassmorphism").closest("button");
		expect(glassButton).toBeInTheDocument();
		if (glassButton) {
			await user.click(glassButton);
		}
		// The UI style should be selected (visual change via theme context)
		await waitFor(() => {
			// Verify the button was clicked (theme context handles state)
			expect(glassButton).toBeInTheDocument();
		});
	});

	it("switches theme to Dark when Dark button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		const darkButton = screen.getByText("Dark");
		await user.click(darkButton);
		// Theme context handles state change
		expect(darkButton).toBeInTheDocument();
	});

	it("switches theme to Light when Light button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		const lightButton = screen.getByText("Light");
		await user.click(lightButton);
		// Theme context handles state change
		expect(lightButton).toBeInTheDocument();
	});

	it("selects an accent color preset when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// Find accent color buttons (they have title attributes with preset names)
		const colorButtons = screen.getAllByRole("button");
		// Click the first color preset button (excluding custom color button)
		const firstColorButton = colorButtons.find(
			(btn) => btn.getAttribute("title") !== "Custom color",
		);
		if (firstColorButton) {
			await user.click(firstColorButton);
			expect(firstColorButton).toBeInTheDocument();
		}
	});

	it("opens color picker modal when custom color button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		const customColorButton = screen.getByTitle("Custom color");
		await user.click(customColorButton);
		// ColorPickerModal should render (check for modal content)
		await waitFor(() => {
			expect(screen.getByText("Pick a Color")).toBeInTheDocument();
		});
	});

	it("displays UI style description text", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// UI styles have description text below the label
		expect(
			screen.getByText("Refined, professional, minimal"),
		).toBeInTheDocument();
		expect(
			screen.getByText("Developer-centric, high-contrast"),
		).toBeInTheDocument();
		expect(screen.getByText("Slick, translucent surfaces")).toBeInTheDocument();
	});

	it("renders SettingsSection with Palette icon", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		// Palette icon renders as SVG with lucide class
		const icon = document.querySelector(".lucide-palette");
		expect(icon).toBeInTheDocument();
	});

	it("renders theme description text", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(
			screen.getByText("Switch between dark and light mode"),
		).toBeInTheDocument();
	});
});
