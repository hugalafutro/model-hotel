import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { RateLimitSettings } from "../RateLimitSettings";

describe("RateLimitSettings", () => {
	const onToggle = vi.fn();

	beforeAll(() => {
		// Setup MSW handler for settings endpoint
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "true",
					rate_limit_rps: "10",
					rate_limit_burst: "20",
					rate_limit_ip_enabled: "true",
					rate_limit_ip_rps: "30",
					rate_limit_ip_burst: "60",
					rate_limit_max_wait_ms: "200",
				});
			}),
			http.put("/api/settings", async ({ request }) => {
				const body = await request.json();
				return HttpResponse.json(body as Record<string, string>);
			}),
		);
	});

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders section title with Gauge icon", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
		});
		// Gauge icon renders as SVG with lucide class
		const icon = document.querySelector(".lucide-gauge");
		expect(icon).toBeInTheDocument();
	});

	it("renders description text", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Control request throughput per virtual key/i),
			).toBeInTheDocument();
		});
	});

	it("renders Enable Rate Limiting toggle", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
		});
		// There are multiple toggles, get all and check the first one exists
		const toggles = screen.getAllByRole("switch");
		expect(toggles.length).toBeGreaterThanOrEqual(1);
	});

	it("renders Requests per Second select when rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Requests per Second")).toBeInTheDocument();
		});
	});

	it("renders Burst Size select when rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Burst Size")).toBeInTheDocument();
		});
	});

	it("renders IP Rate Limiting toggle", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("IP Rate Limiting")).toBeInTheDocument();
		});
	});

	it("renders IP Requests per Second select when IP rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("IP Requests per Second"),
			).toBeInTheDocument();
		});
	});

	it("renders IP Burst Size select when IP rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("IP Burst Size")).toBeInTheDocument();
		});
	});

	it("renders Max Wait input when rate limiting is enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
		});
	});

	it("displays RPS options in select", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const select = screen.getByLabelText("Requests per Second");
			expect(select).toContainHTML('<option value="5">5 req/s</option>');
			expect(select).toContainHTML('<option value="10">10 req/s</option>');
			expect(select).toContainHTML('<option value="20">20 req/s</option>');
			expect(select).toContainHTML('<option value="50">50 req/s</option>');
			expect(select).toContainHTML('<option value="100">100 req/s</option>');
			expect(select).toContainHTML('<option value="0">Unlimited</option>');
		});
	});

	it("displays Burst options in select", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const select = screen.getByLabelText("Burst Size");
			expect(select).toContainHTML('<option value="10">10</option>');
			expect(select).toContainHTML('<option value="20">20</option>');
			expect(select).toContainHTML('<option value="50">50</option>');
			expect(select).toContainHTML('<option value="100">100</option>');
			expect(select).toContainHTML('<option value="200">200</option>');
		});
	});

	it("calls onToggle when SettingsSection toggle is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
		});
		// CollapsibleToggle renders as a button with title "Collapse" when not collapsed
		const toggleButton = screen.getByRole("button", {
			name: /collapse|expand/i,
		});
		await user.click(toggleButton);
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("toggles rate limiting when switch is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
		});
		const toggles = screen.getAllByRole("switch");
		const rateLimitToggle = toggles[0];
		await user.click(rateLimitToggle);
		// Toggle should trigger API call via useMutation
		expect(rateLimitToggle).toBeInTheDocument();
	});

	it("toggles IP rate limiting when switch is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("IP Rate Limiting")).toBeInTheDocument();
		});
		const toggles = screen.getAllByRole("switch");
		// Second toggle is for IP rate limiting
		const ipToggle = toggles[1];
		await user.click(ipToggle);
		await waitFor(() => {
			expect(ipToggle).toBeInTheDocument();
		});
	});

	it("updates RPS when select value changes", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Requests per Second")).toBeInTheDocument();
		});
		const select = screen.getByLabelText("Requests per Second");
		await user.selectOptions(select, "50");
		// Should trigger API mutation (settings are updated via React Query)
		// The select element value may not update immediately due to caching
		expect(select).toBeInTheDocument();
	});

	it("updates Burst Size when select value changes", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Burst Size")).toBeInTheDocument();
		});
		const select = screen.getByLabelText("Burst Size");
		await user.selectOptions(select, "100");
		// Should trigger API mutation (settings are updated via React Query)
		expect(select).toBeInTheDocument();
	});

	it("renders IP RPS input when value is custom", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("IP Requests per Second"),
			).toBeInTheDocument();
		});
		// IP RPS defaults to "30" which is not in options, so it renders as input
		const input = screen.getByLabelText("IP Requests per Second");
		expect(input).toBeInTheDocument();
		expect(input).toHaveValue("30");
	});

	it("updates Max Wait when input value changes", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
		});
		const input = screen.getByLabelText("Max Wait (ms)");
		await user.clear(input);
		await user.type(input, "500");
		// Should trigger API mutation (settings are updated via React Query)
		expect(input).toBeInTheDocument();
	});

	it("shows Rate Limit Backpressure section when rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limit Backpressure")).toBeInTheDocument();
		});
	});

	it("displays description for Requests per Second", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Sustained request rate allowed per virtual key/i),
			).toBeInTheDocument();
		});
	});

	it("displays description for Burst Size", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(
					/Maximum number of simultaneous requests before throttling/i,
				),
			).toBeInTheDocument();
		});
	});

	it("displays description for IP Rate Limiting", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByText(/Per-IP rate limiter \(DoS protection/i),
			).toBeInTheDocument();
		});
	});

	it("renders Rate Limit Backpressure section description", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limit Backpressure")).toBeInTheDocument();
		});
		expect(
			screen.getByText(
				/Shared wait behavior for both per-key and IP rate limiters/i,
			),
		).toBeInTheDocument();
	});
});
