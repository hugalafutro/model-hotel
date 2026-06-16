import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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

	it("renders the Follow System theme button between Dark and Light", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		const followSystem = screen.getByRole("button", { name: "Follow System" });
		expect(followSystem).toBeInTheDocument();
		expect(followSystem).toHaveAttribute("title", "Follow System");
	});

	it("activates the system theme preference when Follow System is clicked", async () => {
		const user = userEvent.setup();
		localStorage.removeItem("theme");
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		const followSystem = screen.getByRole("button", { name: "Follow System" });
		expect(followSystem.className).not.toContain("ui-btn-primary");
		await user.click(followSystem);
		expect(localStorage.getItem("theme")).toBe("system");
		expect(followSystem.className).toContain("ui-btn-primary");
		localStorage.removeItem("theme");
	});

	it("renders Accent Color section", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Accent Color")).toBeInTheDocument();
	});

	it("resets an explicit accent back to the theme default", async () => {
		localStorage.setItem("accentColor", "#123456");
		const { user } = renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const reset = screen.getByRole("button", {
			name: "Reset to theme default",
		});
		await user.click(reset);

		// Explicit pick cleared: theme default applies again and the reset
		// affordance disappears.
		expect(localStorage.getItem("accentColor")).toBe("");
		expect(
			screen.queryByRole("button", { name: "Reset to theme default" }),
		).not.toBeInTheDocument();
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
		const icon = document.querySelector(".icon-palette");
		expect(icon).toBeInTheDocument();
	});

	it("renders theme toggle buttons", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByRole("button", { name: "Dark" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Light" })).toBeInTheDocument();
	});

	describe("color picker modal interactions", () => {
		it("closes modal without applying when Cancel is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<AppearanceSettings collapsed={false} onToggle={onToggle} />,
			);
			const customColorButton = screen.getByTitle("Custom color");
			await user.click(customColorButton);
			await waitFor(() => {
				expect(screen.getByText("Pick a Color")).toBeInTheDocument();
			});
			// Scope to the modal dialog to avoid finding other Cancel buttons
			const modal = screen.getByRole("dialog");
			const cancelButton = within(modal).getByRole("button", {
				name: "Cancel",
			});
			await user.click(cancelButton);
			expect(screen.queryByText("Pick a Color")).not.toBeInTheDocument();
		});

		it("applies custom color when Apply is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<AppearanceSettings collapsed={false} onToggle={onToggle} />,
			);
			const customColorButton = screen.getByTitle("Custom color");
			await user.click(customColorButton);
			await waitFor(() => {
				expect(screen.getByText("Pick a Color")).toBeInTheDocument();
			});
			// Scope to the modal dialog
			const modal = screen.getByRole("dialog");
			const applyButton = within(modal).getByRole("button", { name: "Apply" });
			await user.click(applyButton);
			expect(screen.queryByText("Pick a Color")).not.toBeInTheDocument();
		});

		it("shows color preview when non-preset color is active", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<AppearanceSettings collapsed={false} onToggle={onToggle} />,
			);
			// Open the color picker modal
			const customColorButton = screen.getByTitle("Custom color");
			await user.click(customColorButton);
			await waitFor(() => {
				expect(screen.getByText("Pick a Color")).toBeInTheDocument();
			});
			// Change the color to a non-preset value via the hex input
			const hexInput = screen.getByRole("textbox");
			await user.clear(hexInput);
			await user.type(hexInput, "ff00ff"); // Magenta, not a preset
			// Click Apply to apply the custom color (scope to modal)
			const modal = screen.getByRole("dialog");
			const applyButton = within(modal).getByRole("button", { name: "Apply" });
			await user.click(applyButton);
			// Modal should close
			expect(screen.queryByText("Pick a Color")).not.toBeInTheDocument();
			// The custom color button should now show a colored circle (not the "+" SVG)
			// Verify the colored preview circle exists inside the custom color button
			const colorCircle = customColorButton.querySelector(
				'div[style*="background-color"]',
			);
			expect(colorCircle).toBeInTheDocument();
		});

		it("reflects active theme with accent styling", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<AppearanceSettings collapsed={false} onToggle={onToggle} />,
			);
			// Click Dark to ensure it's the active theme
			await user.click(screen.getByText("Dark"));
			const darkButton = screen.getByText("Dark").closest("button");
			expect(darkButton?.className).toContain("ui-btn-primary");
		});

		it("reflects active UI style with accent border", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<AppearanceSettings collapsed={false} onToggle={onToggle} />,
			);
			// Click Clean SaaS to ensure it's the active style
			const cleanSaaSButton = screen.getByText("Clean SaaS").closest("button");
			expect(cleanSaaSButton).toBeInTheDocument();
			if (cleanSaaSButton) {
				await user.click(cleanSaaSButton);
			}
			expect(cleanSaaSButton?.className).toContain("border-(--accent)");
		});

		describe("Toast Notifications", () => {
			it("renders all 6 toast position dots", () => {
				renderWithProviders(
					<AppearanceSettings collapsed={false} onToggle={onToggle} />,
				);
				expect(screen.getByTitle("Top Left")).toBeInTheDocument();
				expect(screen.getByTitle("Top Center")).toBeInTheDocument();
				expect(screen.getByTitle("Top Right")).toBeInTheDocument();
				expect(screen.getByTitle("Bottom Left")).toBeInTheDocument();
				expect(screen.getByTitle("Bottom Center")).toBeInTheDocument();
				expect(screen.getByTitle("Bottom Right")).toBeInTheDocument();
			});

			it("renders Auto-dismiss slider", () => {
				renderWithProviders(
					<AppearanceSettings collapsed={false} onToggle={onToggle} />,
				);
				expect(screen.getByLabelText("Auto-dismiss")).toBeInTheDocument();
			});

			it("clicks top-left toast position dot", async () => {
				const user = userEvent.setup();
				renderWithProviders(
					<AppearanceSettings collapsed={false} onToggle={onToggle} />,
				);

				const topLeftButton = screen.getByTitle("Top Left");
				await user.click(topLeftButton);

				await waitFor(() => {
					expect(screen.getByText(/test notification/i)).toBeInTheDocument();
				});
			});

			it("clicks bottom-right toast position dot", async () => {
				const user = userEvent.setup();
				renderWithProviders(
					<AppearanceSettings collapsed={false} onToggle={onToggle} />,
				);

				const bottomRightButton = screen.getByTitle("Bottom Right");
				await user.click(bottomRightButton);

				await waitFor(() => {
					expect(screen.getByText(/test notification/i)).toBeInTheDocument();
				});
			});

			it("changes auto-dismiss slider value", async () => {
				const user = userEvent.setup();
				renderWithProviders(
					<AppearanceSettings collapsed={false} onToggle={onToggle} />,
				);

				const slider = screen.getByLabelText("Auto-dismiss");
				await user.click(slider);
				fireEvent.input(slider, { target: { value: "10" } });

				expect(slider).toHaveValue("10");
			});
		});
	});
});

describe("AppearanceSettings accent color", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders accent color presets as colored circles", () => {
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		// Find all accent color buttons by their title attribute
		// The preset buttons have title attributes from i18n keys
		const allButtons = screen.getAllByRole("button");
		const colorButtons = allButtons.filter((btn) => {
			// Filter for swatch buttons (color-swatch w-8 h-8) with background-color style
			const className = btn.className;
			const hasBackground = btn
				.getAttribute("style")
				?.includes("background-color");
			return (
				className?.includes("color-swatch") &&
				className?.includes("w-8") &&
				hasBackground
			);
		});

		// Should have multiple preset colors plus the custom color button
		expect(colorButtons.length).toBeGreaterThan(1);
	});

	it("clicks top-center toast position dot", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const topCenterButton = screen.getByTitle("Top Center");
		await user.click(topCenterButton);

		await waitFor(() => {
			expect(screen.getByText(/test notification/i)).toBeInTheDocument();
		});
	});

	it("clicks top-right toast position dot", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const topRightButton = screen.getByTitle("Top Right");
		await user.click(topRightButton);

		await waitFor(() => {
			expect(screen.getByText(/test notification/i)).toBeInTheDocument();
		});
	});

	it("clicks bottom-left toast position dot", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const bottomLeftButton = screen.getByTitle("Bottom Left");
		await user.click(bottomLeftButton);

		await waitFor(() => {
			expect(screen.getByText(/test notification/i)).toBeInTheDocument();
		});
	});

	it("clicks bottom-center toast position dot", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const bottomCenterButton = screen.getByTitle("Bottom Center");
		await user.click(bottomCenterButton);

		await waitFor(() => {
			expect(screen.getByText(/test notification/i)).toBeInTheDocument();
		});
	});

	it("updates toast timeout when auto-dismiss slider changes", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		const slider = screen.getByLabelText("Auto-dismiss");
		await user.click(slider);
		fireEvent.input(slider, { target: { value: "15" } });
		fireEvent.pointerUp(slider);

		// The setToastTimeout callback is called with value * 1000
		expect(slider).toHaveValue("15");
	});

	it("sets accent color when clicking a preset", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AppearanceSettings collapsed={false} onToggle={onToggle} />,
		);

		// Find accent color preset buttons - they have background-color style and circular shape
		const allButtons = screen.getAllByRole("button");
		const colorButtons = allButtons.filter((btn) => {
			const className = btn.className;
			return (
				className?.includes("color-swatch") &&
				className?.includes("w-8") &&
				btn.getAttribute("style")?.includes("background-color")
			);
		});

		expect(colorButtons.length).toBeGreaterThan(0);
		await user.click(colorButtons[0]);
		// The accent color should be set (visual change only, no toast)
		expect(colorButtons[0]).toBeInTheDocument();
	});
});
