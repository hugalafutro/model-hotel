import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMockLogEntry, createMockLogs } from "../../test/logFixtures";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Logs } from "../Logs";

// The paginated request-logs table carries an IP column fed by
// request_logs.client_ip; rows predating the column fall back to a dash.

describe("Logs IP column", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		localStorage.setItem("requestLogsViewMode", "paginate");
	});

	it("renders the IP header and the row's client IP in paginate mode", async () => {
		server.use(
			http.get("/api/logs", () =>
				HttpResponse.json(
					createMockLogs([
						createMockLogEntry({
							model_id: "ip-model",
							client_ip: "203.0.113.7",
						}),
					]),
				),
			),
		);

		renderWithProviders(<Logs />);

		await waitFor(() => {
			expect(screen.getByText("ip-model")).toBeInTheDocument();
		});
		expect(screen.getByText("IP")).toBeInTheDocument();
		expect(screen.getByText("203.0.113.7")).toBeInTheDocument();
	});
});
