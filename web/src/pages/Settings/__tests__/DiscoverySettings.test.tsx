import { fireEvent, screen, waitFor } from "@testing-library/react";
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const slider = screen.getByRole("slider", {
				name: "Discovery Interval",
			});
			expect(slider).toBeInTheDocument();
		});

		const slider = screen.getByRole("slider", {
			name: "Discovery Interval",
		});
		fireEvent.change(slider, { target: { value: 12 } });
		fireEvent.pointerUp(slider);

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on mutation failure", async () => {
		server.use(http.put("/api/settings", () => HttpResponse.error()));

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const slider = screen.getByRole("slider", {
				name: "Discovery Interval",
			});
			expect(slider).toBeInTheDocument();
		});

		const slider = screen.getByRole("slider", {
			name: "Discovery Interval",
		});
		fireEvent.change(slider, { target: { value: 12 } });
		fireEvent.pointerUp(slider);

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("shows description with disable note when interval is 0", async () => {
		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
			expect(screen.getByText(/0 to disable/i)).toBeInTheDocument();
		});
	});

	it("renders discovery interval slider", () => {
		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		const slider = screen.getByRole("slider", {
			name: "Discovery Interval",
		});

		expect(slider).toBeInTheDocument();
		expect(slider).toHaveAttribute("min", "0");
		expect(slider).toHaveAttribute("max", "48");
		expect(slider).toHaveAttribute("step", "0.5");
	});

	it("toggles Discover on Startup and calls mutation with correct payload", async () => {
		const user = userEvent.setup();

		let capturedPayload: Record<string, string> | undefined;

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_on_startup: "true",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
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
		expect(capturedPayload).toEqual({ discovery_on_startup: "false" });
	});

	it("toggles Discover on Provider Creation and calls mutation with correct payload", async () => {
		const user = userEvent.setup();

		let capturedPayload: Record<string, string> | undefined;

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					discovery_on_provider_create: "true",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
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
		expect(capturedPayload).toEqual({ discovery_on_provider_create: "false" });
	});

	it("disables controls while mutation is pending", async () => {
		server.use(
			http.put("/api/settings", async () => {
				await new Promise((resolve) => setTimeout(resolve, 200));
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<DiscoverySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const slider = screen.getByRole("slider", {
				name: "Discovery Interval",
			});
			expect(slider).toBeInTheDocument();
		});

		const slider = screen.getByRole("slider", {
			name: "Discovery Interval",
		});
		const discoverOnStartupToggle = screen.getByRole("switch", {
			name: /discover on startup/i,
		});
		const discoverOnCreateToggle = screen.getByRole("switch", {
			name: /discover on provider creation/i,
		});

		expect(slider).not.toBeDisabled();
		expect(discoverOnStartupToggle).not.toBeDisabled();
		expect(discoverOnCreateToggle).not.toBeDisabled();

		fireEvent.change(slider, { target: { value: 12 } });
		fireEvent.pointerUp(slider);

		// Mutation starts immediately (no debounce), controls become disabled
		await waitFor(() => {
			expect(slider).toBeDisabled();
			expect(discoverOnStartupToggle).toBeDisabled();
			expect(discoverOnCreateToggle).toBeDisabled();
		});

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});

		expect(slider).not.toBeDisabled();
		expect(discoverOnStartupToggle).not.toBeDisabled();
		expect(discoverOnCreateToggle).not.toBeDisabled();
	});

	it("shows error toast when toggle mutation fails", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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

	describe("Discover All button", () => {
		it("shows Play icon when not discovering", () => {
			renderWithProviders(
				<DiscoverySettings collapsed={false} onToggle={() => {}} />,
			);

			const button = screen.getByRole("button", {
				name: /discover all models/i,
			});
			expect(button).toBeInTheDocument();
			expect(button).not.toBeDisabled();
			expect(button.querySelector("[data-testid='spinner']")).toBeNull();
		});

		it("shows Spinner and disables button during discovery", async () => {
			server.use(
				http.post("/api/providers/discover-all", async () => {
					await new Promise((resolve) => setTimeout(resolve, 2000));
					return HttpResponse.json({
						succeeded: 1,
						failed: 0,
						discovered: 5,
						results: [],
					});
				}),
			);

			renderWithProviders(
				<DiscoverySettings collapsed={false} onToggle={() => {}} />,
			);

			const button = screen.getByRole("button", {
				name: /discover all models/i,
			});
			await userEvent.setup().click(button);

			await waitFor(() => {
				expect(
					button.querySelector("[data-testid='spinner']"),
				).toBeInTheDocument();
			});
			expect(button).toBeDisabled();
		});

		it("shows success toast when discovery completes", async () => {
			server.use(
				http.post("/api/providers/discover-all", () =>
					HttpResponse.json({
						succeeded: 2,
						failed: 0,
						discovered: 10,
						results: [],
					}),
				),
			);

			renderWithProviders(
				<DiscoverySettings collapsed={false} onToggle={() => {}} />,
			);

			const button = screen.getByRole("button", {
				name: /discover all models/i,
			});
			await userEvent.setup().click(button);

			await waitFor(() => {
				expect(screen.getByText("Discovery complete")).toBeInTheDocument();
			});
		});

		it("shows error toast when discovery fails", async () => {
			server.use(
				http.post("/api/providers/discover-all", () => HttpResponse.error()),
			);

			renderWithProviders(
				<DiscoverySettings collapsed={false} onToggle={() => {}} />,
			);

			const button = screen.getByRole("button", {
				name: /discover all models/i,
			});
			await userEvent.setup().click(button);

			await waitFor(() => {
				expect(screen.getByText(/Discovery failed/i)).toBeInTheDocument();
			});
		});

		it("disables settings controls while discovery is in progress", async () => {
			// Gate the response so the mutation stays pending exactly until the
			// disabled-state assertions are done, then settles inside the test —
			// a handler that outlives the test rejects after jsdom teardown and
			// crashes in the onError toast ("window is not defined").
			let release: () => void = () => {};
			const gate = new Promise<void>((resolve) => {
				release = resolve;
			});
			server.use(
				http.post("/api/providers/discover-all", async () => {
					await gate;
					return HttpResponse.json({
						succeeded: 1,
						failed: 0,
						discovered: 5,
						results: [],
					});
				}),
			);

			renderWithProviders(
				<DiscoverySettings collapsed={false} onToggle={() => {}} />,
			);

			const button = screen.getByRole("button", {
				name: /discover all models/i,
			});

			const slider = screen.getByRole("slider", {
				name: "Discovery Interval",
			});
			const discoverOnStartupToggle = screen.getByRole("switch", {
				name: /discover on startup/i,
			});

			expect(slider).not.toBeDisabled();
			expect(discoverOnStartupToggle).not.toBeDisabled();

			await userEvent.setup().click(button);

			await waitFor(() => {
				expect(slider).toBeDisabled();
				expect(discoverOnStartupToggle).toBeDisabled();
			});

			// Let the mutation settle before the test ends.
			release();
			await waitFor(() => {
				expect(screen.getByText("Discovery complete")).toBeInTheDocument();
			});
		});
	});
});
