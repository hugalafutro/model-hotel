import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "../../../test/utils";
import { SidebarQuotaSettings } from "../SidebarQuotaSettings";

describe("SidebarQuotaSettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	it("renders with default collapsed state", () => {
		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		expect(screen.getByText("Sidebar Quotas")).toBeInTheDocument();
		expect(screen.getByText("Show Quotas Pill")).toBeInTheDocument();
		expect(screen.getByText("Refresh Interval")).toBeInTheDocument();
		expect(
			screen.getByText(/Configure how often provider quota/i),
		).toBeInTheDocument();
	});

	it("displays correct toggle state when localStorage has value", () => {
		localStorage.setItem("sidebarQuotaDisabled", "true");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeInTheDocument();
		expect(toggle).not.toBeChecked();
	});

	it("displays correct toggle state when localStorage is false", () => {
		localStorage.setItem("sidebarQuotaDisabled", "false");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeInTheDocument();
		expect(toggle).toBeChecked();
	});

	it("displays default refresh interval when localStorage is empty", () => {
		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select.value).toBe("5");
	});

	it("displays refresh interval from localStorage", () => {
		localStorage.setItem("sidebarQuotaRefreshMin", "10");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select.value).toBe("10");
	});

	it("toggles quota display and dispatches event", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeChecked();

		await user.click(toggle);

		expect(localStorage.getItem("sidebarQuotaDisabled")).toBe("true");
		expect(dispatchSpy).toHaveBeenCalled();
		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaToggle" }),
		);
		expect(screen.getByText(/Sidebar quotas disabled/i)).toBeInTheDocument();
	});

	it("enables quota display when toggled from disabled", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		localStorage.setItem("sidebarQuotaDisabled", "true");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).not.toBeChecked();

		await user.click(toggle);

		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaToggle" }),
		);
		expect(screen.getByText(/Sidebar quotas enabled/i)).toBeInTheDocument();
	});

	it("changes refresh interval and dispatches event", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "15");

		expect(localStorage.getItem("sidebarQuotaRefreshMin")).toBe("15");
		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "sidebarQuotaRefreshChange" }),
		);
		expect(
			screen.getByText(/Quota refresh set to every 15 minutes/i),
		).toBeInTheDocument();
	});

	it("shows correct toast for 1 minute interval", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "1");

		expect(
			screen.getByText(/Quota refresh set to every 1 minute/i),
		).toBeInTheDocument();
	});

	it("shows correct toast when disabling auto-refresh", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "0");

		expect(
			screen.getByText(/Sidebar quota auto-refresh disabled/i),
		).toBeInTheDocument();
	});

	it("disables refresh select when quota is disabled", async () => {
		const user = userEvent.setup();

		localStorage.setItem("sidebarQuotaDisabled", "true");

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select).toBeDisabled();

		await user.click(screen.getByRole("switch"));

		expect(select).not.toBeDisabled();
	});

	it("renders all refresh interval options", () => {
		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;

		expect(select.options).toHaveLength(7);
		expect(select.options[0].value).toBe("1");
		expect(select.options[1].value).toBe("2");
		expect(select.options[2].value).toBe("5");
		expect(select.options[3].value).toBe("10");
		expect(select.options[4].value).toBe("15");
		expect(select.options[5].value).toBe("30");
		expect(select.options[6].value).toBe("0");
	});

	it("handles localStorage errors gracefully", async () => {
		const user = userEvent.setup();
		vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new Error("Storage error");
		});

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(screen.getByText(/Sidebar quotas disabled/i)).toBeInTheDocument();
	});

	it("handles localStorage getItem errors gracefully", () => {
		vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
			throw new Error("Storage error");
		});

		renderWithProviders(
			<SidebarQuotaSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeChecked();
	});
});
