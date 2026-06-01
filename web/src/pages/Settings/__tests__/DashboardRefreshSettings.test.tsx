import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "../../../test/utils";
import { DashboardRefreshSettings } from "../DashboardRefreshSettings";

describe("DashboardRefreshSettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	it("renders with default collapsed state", () => {
		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		expect(screen.getByText("Dashboard Refresh")).toBeInTheDocument();
		expect(screen.getByText("Refresh Interval")).toBeInTheDocument();
		expect(
			screen.getByText(/Configure how often the dashboard stats/i),
		).toBeInTheDocument();
	});

	it("displays default refresh interval when localStorage is empty", () => {
		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select.value).toBe("30");
	});

	it("displays refresh interval from localStorage", () => {
		localStorage.setItem("dashboardRefreshSec", "60");

		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select.value).toBe("60");
	});

	it("changes refresh interval and dispatches event", async () => {
		const user = userEvent.setup();
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		renderWithProviders(
			<DashboardRefreshSettings collapsed onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "120");

		expect(localStorage.getItem("dashboardRefreshSec")).toBe("120");
		expect(dispatchSpy).toHaveBeenCalledWith(
			expect.objectContaining({ type: "dashboardRefreshChange" }),
		);
		expect(
			screen.getByText(/Dashboard refresh set to every 120 seconds/),
		).toBeInTheDocument();
	});

	it("shows correct toast for 10 seconds interval", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "10");

		expect(
			screen.getByText(/Dashboard refresh set to every 10 seconds/),
		).toBeInTheDocument();
	});

	it("shows correct toast when disabling auto-refresh", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "0");

		expect(
			screen.getByText(/Dashboard auto-refresh disabled/i),
		).toBeInTheDocument();
	});

	it("renders all refresh interval options", () => {
		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;

		expect(select.options).toHaveLength(7);
		expect(select.options[0].value).toBe("10");
		expect(select.options[1].value).toBe("30");
		expect(select.options[2].value).toBe("60");
		expect(select.options[3].value).toBe("120");
		expect(select.options[4].value).toBe("300");
		expect(select.options[5].value).toBe("600");
		expect(select.options[6].value).toBe("0");
	});

	it("handles localStorage errors gracefully", async () => {
		const user = userEvent.setup();
		vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new Error("Storage error");
		});

		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(select, "60");

		expect(
			screen.getByText(/Dashboard refresh set to every 60 seconds/),
		).toBeInTheDocument();
	});

	it("handles localStorage getItem errors gracefully", () => {
		vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
			throw new Error("Storage error");
		});

		renderWithProviders(
			<DashboardRefreshSettings collapsed={false} onToggle={() => {}} />,
		);

		const select = screen.getByLabelText(
			/Refresh Interval/i,
		) as HTMLSelectElement;
		expect(select.value).toBe("30");
	});
});
