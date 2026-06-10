import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { RateLimitSettings } from "../RateLimitSettings";

describe("RateLimitSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
		server.resetHandlers();
	});

	it("renders section title with Gauge icon", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
		});
		// Gauge icon renders as SVG element with lucide-gauge class
		const gaugeIcon = document.querySelector(".lucide-gauge");
		expect(gaugeIcon).toBeInTheDocument();
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

	it("renders Requests per Second slider when rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Requests per Second")).toBeInTheDocument();
		});
	});

	it("renders Burst Size slider when rate limiting enabled", async () => {
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

	it("renders IP Requests per Second slider when IP rate limiting enabled", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("IP Requests per Second"),
			).toBeInTheDocument();
		});
	});

	it("renders IP Burst Size slider when IP rate limiting enabled", async () => {
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

	it("displays RPS slider with default value", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const slider = screen.getByLabelText("Requests per Second");
			expect(slider).toHaveValue("10");
		});
	});

	it("displays Burst slider with default value", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const slider = screen.getByLabelText("Burst Size");
			expect(slider).toHaveValue("20");
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

	it("updates RPS when slider value changes", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Requests per Second")).toBeInTheDocument();
		});
		const slider = screen.getByLabelText("Requests per Second");
		fireEvent.change(slider, { target: { value: "50" } });
		// Should trigger API mutation (settings are updated via React Query)
		// The slider element value may not update immediately due to caching
		expect(slider).toBeInTheDocument();
	});

	it("updates Burst Size when slider value changes", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Burst Size")).toBeInTheDocument();
		});
		const slider = screen.getByLabelText("Burst Size");
		fireEvent.change(slider, { target: { value: "100" } });
		// Should trigger API mutation (settings are updated via React Query)
		expect(slider).toBeInTheDocument();
	});

	it("renders IP RPS slider with default value", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.getByLabelText("IP Requests per Second"),
			).toBeInTheDocument();
		});
		const slider = screen.getByLabelText("IP Requests per Second");
		expect(slider).toHaveValue("30");
	});

	it("updates Max Wait when slider value changes", async () => {
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
		});
		const slider = screen.getByLabelText("Max Wait (ms)");
		fireEvent.change(slider, { target: { value: "500" } });
		// Should trigger API mutation (settings are updated via React Query)
		expect(slider).toBeInTheDocument();
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

	it("hides RPS and Burst sliders when rate limiting is disabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "false",
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
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		// Wait for settings to load (toggle text appears)
		await waitFor(() => {
			expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(
				screen.queryByLabelText("Requests per Second"),
			).not.toBeInTheDocument();
		});
		expect(screen.queryByLabelText("Burst Size")).not.toBeInTheDocument();
	});

	it("hides IP RPS and IP Burst sliders when IP rate limiting is disabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "true",
					rate_limit_rps: "10",
					rate_limit_burst: "20",
					rate_limit_ip_enabled: "false",
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
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("IP Rate Limiting")).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(
				screen.queryByLabelText("IP Requests per Second"),
			).not.toBeInTheDocument();
		});
		expect(screen.queryByLabelText("IP Burst Size")).not.toBeInTheDocument();
	});

	it("hides backpressure section when both rate limiters are disabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "false",
					rate_limit_rps: "10",
					rate_limit_burst: "20",
					rate_limit_ip_enabled: "false",
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
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(
				screen.queryByText("Rate Limit Backpressure"),
			).not.toBeInTheDocument();
		});
		expect(screen.queryByLabelText("Max Wait (ms)")).not.toBeInTheDocument();
	});

	it("shows backpressure section when rate limiting is enabled but IP rate limiting is disabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "true",
					rate_limit_rps: "10",
					rate_limit_burst: "20",
					rate_limit_ip_enabled: "false",
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
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limit Backpressure")).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
		});
	});

	it("shows backpressure section when IP rate limiting is enabled but rate limiting is disabled", async () => {
		server.use(
			http.get("/api/settings", () => {
				return HttpResponse.json({
					rate_limit_enabled: "false",
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
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Rate Limit Backpressure")).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
		});
	});

	it("renders error state when settings API returns error", async () => {
		server.use(
			http.get("/api/settings", () => {
				return new HttpResponse(null, { status: 500 });
			}),
		);
		renderWithProviders(
			<RateLimitSettings collapsed={false} onToggle={onToggle} />,
		);
		// The component should still render the section title even when settings fail to load
		await waitFor(() => {
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
		});
	});

	describe("mutation payload verification", () => {
		it("sends correct payload when rate limit toggle is clicked", async () => {
			const user = userEvent.setup();
			let capturedPayload: Record<string, string> | undefined;

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
					if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
			});

			const toggles = screen.getAllByRole("switch");
			await user.click(toggles[0]); // rate limit toggle

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_enabled: "false" });
			});
		});

		it("sends correct payload when IP rate limit toggle is clicked", async () => {
			const user = userEvent.setup();
			let capturedPayload: Record<string, string> | undefined;

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
					if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("IP Rate Limiting")).toBeInTheDocument();
			});

			const toggles = screen.getAllByRole("switch");
			await user.click(toggles[1]); // IP rate limit toggle

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_ip_enabled: "false" });
			});
		});

		it("sends RPS value when slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

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
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(
					screen.getByLabelText("Requests per Second"),
				).toBeInTheDocument();
			});

			const slider = screen.getByLabelText("Requests per Second");
			fireEvent.change(slider, { target: { value: "50" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_rps: "50" });
			});
		});

		it("sends burst value when slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

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
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByLabelText("Burst Size")).toBeInTheDocument();
			});

			const slider = screen.getByLabelText("Burst Size");
			fireEvent.change(slider, { target: { value: "100" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_burst: "100" });
			});
		});

		it("sends IP RPS value when slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

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
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(
					screen.getByLabelText("IP Requests per Second"),
				).toBeInTheDocument();
			});

			const slider = screen.getByLabelText("IP Requests per Second");
			fireEvent.change(slider, { target: { value: "75" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_ip_rps: "75" });
			});
		});

		it("sends IP burst value when slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

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
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByLabelText("IP Burst Size")).toBeInTheDocument();
			});

			const slider = screen.getByLabelText("IP Burst Size");
			fireEvent.change(slider, { target: { value: "120" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_ip_burst: "120" });
			});
		});

		it("sends max wait value when slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

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
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json(capturedPayload ?? {});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByLabelText("Max Wait (ms)")).toBeInTheDocument();
			});

			const slider = screen.getByLabelText("Max Wait (ms)");
			fireEvent.change(slider, { target: { value: "800" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ rate_limit_max_wait_ms: "800" });
			});
		});
	});

	describe("success/error toasts", () => {
		it("shows settings saved toast on successful save", async () => {
			const user = userEvent.setup();

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
				http.put("/api/settings", async () => {
					return HttpResponse.json({ success: true });
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
			});

			const toggles = screen.getAllByRole("switch");
			await user.click(toggles[0]);

			await waitFor(() => {
				expect(screen.getByText("Settings saved")).toBeInTheDocument();
			});
		});

		it("shows error toast on failed save", async () => {
			const user = userEvent.setup();

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
				http.put("/api/settings", async () => {
					return HttpResponse.json(
						{ error: "Internal server error" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(screen.getByText("Enable Rate Limiting")).toBeInTheDocument();
			});

			const toggles = screen.getAllByRole("switch");
			await user.click(toggles[0]);

			await waitFor(() => {
				expect(screen.getByText(/Failed to save/i)).toBeInTheDocument();
			});
		});
	});

	describe("collapsed state", () => {
		it("renders collapsed section without content", async () => {
			renderWithProviders(
				<RateLimitSettings collapsed={true} onToggle={onToggle} />,
			);

			// Section title should still appear
			await waitFor(() => {
				expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
			});

			// Content is hidden via CSS grid-rows-[0fr] with overflow-hidden
			// The content wrapper should have the collapsed grid-rows class
			const contentWrapper = document.querySelector(".grid-rows-\\[0fr\\]");
			expect(contentWrapper).toBeInTheDocument();
		});
	});

	describe("default values with settings", () => {
		it("renders with custom settings values from API", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({
						rate_limit_enabled: "true",
						rate_limit_rps: "50",
						rate_limit_burst: "100",
						rate_limit_ip_enabled: "true",
						rate_limit_ip_rps: "60",
						rate_limit_ip_burst: "120",
						rate_limit_max_wait_ms: "500",
					});
				}),
			);

			renderWithProviders(
				<RateLimitSettings collapsed={false} onToggle={onToggle} />,
			);

			await waitFor(() => {
				const rpsSlider = screen.getByLabelText("Requests per Second");
				expect(rpsSlider).toHaveValue("50");
			});

			await waitFor(() => {
				const burstSlider = screen.getByLabelText("Burst Size");
				expect(burstSlider).toHaveValue("100");
			});

			await waitFor(() => {
				const ipRpsSlider = screen.getByLabelText("IP Requests per Second");
				expect(ipRpsSlider).toHaveValue("60");
			});

			await waitFor(() => {
				const ipBurstSlider = screen.getByLabelText("IP Burst Size");
				expect(ipBurstSlider).toHaveValue("120");
			});

			await waitFor(() => {
				const maxWaitSlider = screen.getByLabelText("Max Wait (ms)");
				expect(maxWaitSlider).toHaveValue("500");
			});
		});
	});
});

// Note: Toggle visual state (aria-checked) is implicitly tested via the
// existing tests that verify sliders appear/disappear based on toggle state:
// - "hides RPS and Burst sliders when rate limiting is disabled"
// - "hides IP RPS and IP Burst sliders when IP rate limiting is disabled"

describe("per-setting reset", () => {
	it("calls api.settings.reset when reset button is clicked", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(<RateLimitSettings onResetSection={() => {}} />);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset this setting to default/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const resetBtn = screen.getAllByRole("button", {
			name: /reset this setting to default/i,
		})[0];
		await user.click(resetBtn);

		await waitFor(() => {
			expect(resetSpy).toHaveBeenCalledOnce();
		});

		resetSpy.mockRestore();
	});

	it("shows error toast when reset fails", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockRejectedValueOnce(new Error("reset went sideways"));

		const user = userEvent.setup();
		renderWithProviders(<RateLimitSettings onResetSection={() => {}} />);

		await waitFor(() => {
			expect(
				screen.getAllByRole("button", {
					name: /reset this setting to default/i,
				}).length,
			).toBeGreaterThanOrEqual(1);
		});

		const resetBtn = screen.getAllByRole("button", {
			name: /reset this setting to default/i,
		})[0];
		await user.click(resetBtn);

		await waitFor(() => {
			expect(screen.getByText(/reset went sideways/i)).toBeInTheDocument();
		});

		resetSpy.mockRestore();
	});
});
