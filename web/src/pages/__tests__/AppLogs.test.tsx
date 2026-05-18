import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { AppLogs } from "../AppLogs";

// Mock LogDetailModal
vi.mock("../../components/LogDetailModal", () => ({
	LogDetailModal: ({
		log,
		onClose,
	}: {
		log: { id: string };
		onClose: () => void;
	}) => (
		<div data-testid="log-detail-modal">
			<span>Log Detail: {log.id}</span>
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));

// Mock AccentCalendar to simplify integration test
vi.mock("../../components/AccentCalendar", () => ({
	AccentCalendar: ({
		from,
		to,
		onSelect,
	}: {
		from: string;
		to: string;
		onSelect: (d: string) => void;
	}) => (
		<div data-testid="accent-calendar">
			<span>From: {from}</span>
			<span>To: {to}</span>
			<button type="button" onClick={() => onSelect("2024-06-10")}>
				Select Start
			</button>
			<button type="button" onClick={() => onSelect("2024-06-15")}>
				Select End
			</button>
		</div>
	),
}));

// Mock todayISO
vi.mock("../../components/AccentCalendar.utils", async (importOriginal) => {
	const actual =
		await importOriginal<
			typeof import("../../components/AccentCalendar.utils")
		>();
	return {
		...actual,
		todayISO: vi.fn(() => "2024-06-15"),
	};
});

function mockAppLogsHistory(overrides?: { total?: number }) {
	server.use(
		http.get("/api/logs/app/history", () => {
			const body = {
				entries: [
					{
						timestamp: "2024-06-10T10:00:00Z",
						level: "info",
						source: "proxy",
						message: "test log entry",
					},
				],
				total: overrides?.total ?? 1,
				page: 1,
				per_page: 20,
				level_counts: { info: 1, warning: 0, error: 0 },
				source_counts: { proxy: 1 },
			};
			return HttpResponse.json(body);
		}),
	);
}

describe("AppLogs date picker", () => {
	beforeEach(() => {
		mockAppLogsHistory();
	});

	it("opens date picker when calendar button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		const calendarButton = screen.getByLabelText("Filter by date range");
		await user.click(calendarButton);
		expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
		expect(screen.getByText("Select date range")).toBeInTheDocument();
	});

	it("closes date picker when close button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		const calendarButton = screen.getByLabelText("Filter by date range");
		await user.click(calendarButton);
		expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
		const closeButton = screen.getByLabelText("Close date picker");
		await user.click(closeButton);
		expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
	});

	it("applies date filter when Apply button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		const calendarButton = screen.getByLabelText("Filter by date range");
		await user.click(calendarButton);
		// Select start and end dates via mock calendar
		await user.click(screen.getByText("Select Start"));
		await user.click(screen.getByText("Select End"));
		// Click Apply
		const applyButtons = screen.getAllByRole("button");
		const applyButton = applyButtons.find((b) => b.textContent === "Apply");
		if (applyButton) {
			await user.click(applyButton);
		}
		// After applying, the calendar button should show the active state
		await waitFor(() => {
			expect(screen.getByLabelText(/Date filter:/)).toBeInTheDocument();
		});
	});

	it("clears date filter when Clear button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		// Open and apply a date filter first
		const calendarButton = screen.getByLabelText("Filter by date range");
		await user.click(calendarButton);
		await user.click(screen.getByText("Select Start"));
		await user.click(screen.getByText("Select End"));
		const applyButtons = screen.getAllByRole("button");
		const applyButton = applyButtons.find((b) => b.textContent === "Apply");
		if (applyButton) {
			await user.click(applyButton);
		}
		// Now clear it
		await waitFor(() => {
			expect(screen.getByLabelText(/Date filter:/)).toBeInTheDocument();
		});
		const clearFilterButton = screen.getByLabelText(/Clear date filter/);
		await user.click(clearFilterButton);
		expect(screen.getByLabelText("Filter by date range")).toBeInTheDocument();
	});
});
