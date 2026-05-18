import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { DiscoverySettings } from "../DiscoverySettings";

describe("DiscoverySettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders with default collapsed state", () => {
		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(screen.getByText("Model Discovery")).toBeInTheDocument();
		expect(screen.getByText("Discovery Interval")).toBeInTheDocument();
		expect(screen.getByText("Discover on Startup")).toBeInTheDocument();
		expect(
			screen.getByText("Discover on Provider Creation"),
		).toBeInTheDocument();
		expect(
			screen.getByText(/Configure how and when models are auto-discovered/i),
		).toBeInTheDocument();
	});

	it("displays default values when settings are empty", async () => {
		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({});
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			expect(screen.getByText("Discovery Interval")).toBeInTheDocument();
		});
	});

	it("shows success toast on mutation success", async () => {
		const user = userEvent.setup();

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const intervalSelect = screen.getByLabelText(
				/Discovery Interval/i,
			) as HTMLSelectElement;
			expect(intervalSelect).toBeInTheDocument();
		});

		const intervalSelect = screen.getByLabelText(
			/Discovery Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(intervalSelect, "12h");

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on mutation failure", async () => {
		const user = userEvent.setup();

		server.use(
			http.put("/api/settings", () => HttpResponse.error()),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const intervalSelect = screen.getByLabelText(
				/Discovery Interval/i,
			) as HTMLSelectElement;
			expect(intervalSelect).toBeInTheDocument();
		});

		const intervalSelect = screen.getByLabelText(
			/Discovery Interval/i,
		) as HTMLSelectElement;
		await user.selectOptions(intervalSelect, "12h");

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("shows warning description when interval is 0", async () => {
		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_interval: "0",
				});
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			expect(
				screen.getByText(/Periodic discovery is disabled/i),
			).toBeInTheDocument();
		});
	});

	it("renders all discovery interval options", () => {
		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		const intervalSelect = screen.getByLabelText(
			/Discovery Interval/i,
		) as HTMLSelectElement;

		expect(intervalSelect.options).toHaveLength(6);
		expect(intervalSelect.options[0].value).toBe("30m");
		expect(intervalSelect.options[1].value).toBe("1h");
		expect(intervalSelect.options[2].value).toBe("6h");
		expect(intervalSelect.options[3].value).toBe("12h");
		expect(intervalSelect.options[4].value).toBe("24h");
		expect(intervalSelect.options[5].value).toBe("0");
	});
});
