import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { DiscoverySettings } from "../DiscoverySettings";

describe("DiscoverySettings", () => {
	beforeEach(() => {
		server.resetHandlers();
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

		server.use(http.put("/api/settings", () => HttpResponse.error()));

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

	it("toggles Discover on Startup and calls mutation with correct payload", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_on_startup: "true",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				expect(body).toEqual({ discovery_on_startup: "false" });
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const toggle = screen.getByRole("switch", {
				name: /discover on startup/i,
			});
			expect(toggle).toBeInTheDocument();
		});

		const toggle = screen.getByRole("switch", {
			name: /discover on startup/i,
		});
		expect(toggle).toBeChecked();

		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("toggles Discover on Provider Creation and calls mutation with correct payload", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_on_provider_create: "true",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				expect(body).toEqual({ discovery_on_provider_create: "false" });
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const toggle = screen.getByRole("switch", {
				name: /discover on provider creation/i,
			});
			expect(toggle).toBeInTheDocument();
		});

		const toggle = screen.getByRole("switch", {
			name: /discover on provider creation/i,
		});
		expect(toggle).toBeChecked();

		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("disables toggles and select while mutation is pending", async () => {
		const user = userEvent.setup();

		server.use(
			http.put("/api/settings", async () => {
				await new Promise((resolve) => setTimeout(resolve, 100));
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
		const discoverOnStartupToggle = screen.getByRole("switch", {
			name: /discover on startup/i,
		});
		const discoverOnCreateToggle = screen.getByRole("switch", {
			name: /discover on provider creation/i,
		});

		expect(intervalSelect).not.toBeDisabled();
		expect(discoverOnStartupToggle).not.toBeDisabled();
		expect(discoverOnCreateToggle).not.toBeDisabled();

		await user.selectOptions(intervalSelect, "12h");

		expect(intervalSelect).toBeDisabled();
		expect(discoverOnStartupToggle).toBeDisabled();
		expect(discoverOnCreateToggle).toBeDisabled();

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});

		expect(intervalSelect).not.toBeDisabled();
		expect(discoverOnStartupToggle).not.toBeDisabled();
		expect(discoverOnCreateToggle).not.toBeDisabled();
	});

	it("shows error toast when toggle mutation fails", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_on_startup: "true",
				});
			}),
			http.put("/api/settings", () => HttpResponse.error()),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const toggle = screen.getByRole("switch", {
				name: /discover on startup/i,
			});
			expect(toggle).toBeInTheDocument();
		});

		const toggle = screen.getByRole("switch", {
			name: /discover on startup/i,
		});
		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});
});
