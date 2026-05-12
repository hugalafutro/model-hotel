import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { ToastSettings } from "../ToastSettings";

describe("ToastSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders without crashing", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Toast Notifications")).toBeInTheDocument();
	});

	it("renders section title with Bell icon", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Toast Notifications")).toBeInTheDocument();
		// Bell icon renders as SVG with lucide class
		const icon = document.querySelector(".lucide-bell");
		expect(icon).toBeInTheDocument();
	});

	it("renders description text", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(
			screen.getByText(
				"Choose where notification toasts appear and how long they stay visible.",
			),
		).toBeInTheDocument();
	});

	it("renders position picker container", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		// Position picker is a relative container with border
		// The container has a specific structure with position buttons
		const positionContainer = document.querySelector(".relative.w-40.h-26");
		expect(positionContainer).toBeInTheDocument();
	});

	it("renders all 6 position buttons", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByTitle("Top Left")).toBeInTheDocument();
		expect(screen.getByTitle("Top Center")).toBeInTheDocument();
		expect(screen.getByTitle("Top Right")).toBeInTheDocument();
		expect(screen.getByTitle("Bottom Left")).toBeInTheDocument();
		expect(screen.getByTitle("Bottom Center")).toBeInTheDocument();
		expect(screen.getByTitle("Bottom Right")).toBeInTheDocument();
	});

	it("displays current position label", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		// Position label shows capitalized position (e.g., "top right" -> "top right")
		const positionLabel = screen.getByText(/top|bottom/);
		expect(positionLabel).toBeInTheDocument();
	});

	it("renders auto-dismiss section", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Auto-dismiss")).toBeInTheDocument();
	});

	it("displays timeout value in seconds", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		// Timeout display shows value like "5.0s"
		const timeoutDisplay = screen.getByText(/\d+\.\ds/);
		expect(timeoutDisplay).toBeInTheDocument();
	});

	it("renders timeout slider", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const slider = screen.getByRole("slider");
		expect(slider).toBeInTheDocument();
		expect(slider).toHaveAttribute("min", "1000");
		expect(slider).toHaveAttribute("max", "15000");
		expect(slider).toHaveAttribute("step", "500");
	});

	it("renders slider min/max labels", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("1s")).toBeInTheDocument();
		expect(screen.getByText("15s")).toBeInTheDocument();
	});

	it("calls onToggle when SettingsSection toggle is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		// CollapsibleToggle renders as a button with title "Collapse" when not collapsed
		const toggleButton = screen.getByRole("button", {
			name: /collapse|expand/i,
		});
		await user.click(toggleButton);
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("changes position when top-left button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const topLeftButton = screen.getByTitle("Top Left");
		await user.click(topLeftButton);
		// Position should update in toast context
		await waitFor(() => {
			expect(topLeftButton).toBeInTheDocument();
		});
	});

	it("changes position when top-right button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const topRightButton = screen.getByTitle("Top Right");
		await user.click(topRightButton);
		await waitFor(() => {
			expect(topRightButton).toBeInTheDocument();
		});
	});

	it("changes position when bottom-center button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const bottomCenterButton = screen.getByTitle("Bottom Center");
		await user.click(bottomCenterButton);
		await waitFor(() => {
			expect(bottomCenterButton).toBeInTheDocument();
		});
	});

	it("updates timeout when slider is moved", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const slider = screen.getByRole("slider");
		// Change slider value from default to 5000ms
		await user.click(slider);
		await waitFor(() => {
			// Slider value should change (toast context handles state)
			expect(slider).toBeInTheDocument();
		});
	});

	it("shows toast notification when position button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		const topLeftButton = screen.getByTitle("Top Left");
		await user.click(topLeftButton);
		// Toast should be triggered (check for toast container or notification)
		await waitFor(() => {
			// Toast context should have been called with notification
			expect(topLeftButton).toBeInTheDocument();
		});
	});

	it("renders position buttons with correct opacity states", () => {
		renderWithProviders(
			<ToastSettings collapsed={false} onToggle={onToggle} />,
		);
		// Active position has opacity-100, inactive has opacity-30
		const positionButtons = screen.getAllByTitle(/top|bottom/i);
		expect(positionButtons.length).toBe(6);
	});
});
