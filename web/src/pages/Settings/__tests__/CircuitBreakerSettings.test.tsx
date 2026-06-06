import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { mockSettings } from "../../../test/helpers";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { CircuitBreakerSettings } from "../CircuitBreakerSettings";

describe("CircuitBreakerSettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
		server.resetHandlers();
		localStorage.setItem("adminToken", "test-token");
	});

	it("renders section title with Shield icon", () => {
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Circuit Breaker & Failover")).toBeInTheDocument();
		const icon = document.querySelector(".lucide-shield");
		expect(icon).toBeInTheDocument();
	});

	it("renders description text", () => {
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(
			screen.getByText(
				"Configure how the proxy handles provider failures and rate-limited requests.",
			),
		).toBeInTheDocument();
	});

	it("renders Enable Circuit Breaker toggle with label and description", () => {
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Enable Circuit Breaker")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Temporarily stop routing to providers that are failing",
			),
		).toBeInTheDocument();
	});

	it("renders Failover on Rate Limit toggle with label and description", () => {
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		expect(screen.getByText("Failover on Rate Limit")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Route to next failover group member when a provider returns 429",
			),
		).toBeInTheDocument();
	});

	it("circuit breaker toggle is ON by default when settings undefined", async () => {
		server.use(...mockSettings({ body: {} }));
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const section = screen
				.getByText("Enable Circuit Breaker")
				.closest(".flex.items-center.justify-between");
			const toggle = section?.querySelector("button[role='switch']");
			expect(toggle).toHaveAttribute("aria-checked", "true");
		});
	});

	it("failover on rate limit toggle is OFF by default when settings undefined", async () => {
		server.use(...mockSettings({ body: {} }));
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const section = screen
				.getByText("Failover on Rate Limit")
				.closest(".flex.items-center.justify-between");
			const toggle = section?.querySelector("button[role='switch']");
			expect(toggle).toHaveAttribute("aria-checked", "false");
		});
	});

	it("shows threshold and cooldown when circuit breaker is enabled", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_threshold: "5",
					circuit_breaker_cooldown: "1m0s",
				},
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Failure Threshold")).toBeInTheDocument();
			expect(screen.getByLabelText("Cooldown Period")).toBeInTheDocument();
		});
	});

	it("hides threshold and cooldown when circuit breaker is disabled", async () => {
		server.use(
			...mockSettings({
				body: { circuit_breaker_enabled: "false" },
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(
				screen.queryByLabelText("Failure Threshold"),
			).not.toBeInTheDocument();
			expect(
				screen.queryByLabelText("Cooldown Period"),
			).not.toBeInTheDocument();
		});
	});

	it("displays threshold default value 5", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_threshold: "5",
				},
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const input = screen.getByLabelText(
				"Failure Threshold",
			) as HTMLInputElement;
			expect(input.value).toBe("5");
		});
	});

	it("displays threshold value from settings", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_threshold: "10",
				},
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const input = screen.getByLabelText(
				"Failure Threshold",
			) as HTMLInputElement;
			expect(input.value).toBe("10");
		});
	});

	it("displays cooldown default value in seconds", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_cooldown: "1m0s",
				},
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const input = screen.getByLabelText(
				"Cooldown Period",
			) as HTMLInputElement;
			expect(input.value).toBe("60");
		});
	});

	it("displays cooldown value from settings in seconds", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_cooldown: "5m0s",
				},
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const input = screen.getByLabelText(
				"Cooldown Period",
			) as HTMLInputElement;
			expect(input.value).toBe("300");
		});
	});

	it("failover on rate limit toggle is ON when setting is true", async () => {
		server.use(
			...mockSettings({
				body: { failover_on_rate_limit: "true" },
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			const section = screen
				.getByText("Failover on Rate Limit")
				.closest(".flex.items-center.justify-between");
			const toggle = section?.querySelector("button[role='switch']");
			expect(toggle).toHaveAttribute("aria-checked", "true");
		});
	});

	it("calls mutation when threshold input changes", async () => {
		let mutationCalled = false;

		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_threshold: "5",
				},
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				if (
					typeof body === "object" &&
					body !== null &&
					"circuit_breaker_threshold" in body
				) {
					mutationCalled = true;
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const input = screen.getByLabelText("Failure Threshold");
			expect(input).toBeInTheDocument();
		});

		const input = screen.getByLabelText(
			"Failure Threshold",
		) as HTMLInputElement;
		fireEvent.change(input, { target: { value: "15" } });
		fireEvent.pointerUp(input);

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
		});
	});

	it("calls mutation when cooldown slider changes", async () => {
		let mutationCalled = false;

		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_cooldown: "1m0s",
				},
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				if (
					typeof body === "object" &&
					body !== null &&
					"circuit_breaker_cooldown" in body
				) {
					mutationCalled = true;
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const input = screen.getByLabelText("Cooldown Period");
			expect(input).toBeInTheDocument();
		});

		const input = screen.getByLabelText("Cooldown Period") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "300" } });
		fireEvent.pointerUp(input);

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
		});
	});

	it("shows success toast on mutation success", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_cooldown: "1m0s",
				},
			}),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const input = screen.getByLabelText("Cooldown Period");
			expect(input).toBeInTheDocument();
		});

		const input = screen.getByLabelText("Cooldown Period") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "120" } });
		fireEvent.pointerUp(input);

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on mutation failure", async () => {
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_cooldown: "1m0s",
				},
			}),
			http.put("/api/settings", () => HttpResponse.error()),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const input = screen.getByLabelText("Cooldown Period");
			expect(input).toBeInTheDocument();
		});

		const input = screen.getByLabelText("Cooldown Period") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "120" } });
		fireEvent.pointerUp(input);

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("calls onToggle when collapsible toggle is clicked", async () => {
		const user = userEvent.setup();

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggleButton = screen.getByRole("button", {
			name: /collapse|expand/i,
		});
		await user.click(toggleButton);

		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("renders cooldown slider with correct range", async () => {
		server.use(
			...mockSettings({
				body: { circuit_breaker_enabled: "true" },
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			const input = screen.getByLabelText(
				"Cooldown Period",
			) as HTMLInputElement;
			expect(input.min).toBe("30");
			expect(input.max).toBe("600");
			expect(input.step).toBe("30");
			expect(input.value).toBe("60");
		});
	});

	it("toggles circuit breaker enabled and calls mutation", async () => {
		const user = userEvent.setup();
		let capturedPayload: Record<string, string> | undefined;

		server.use(
			...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = screen.getByRole("switch", {
			name: /enable circuit breaker/i,
		});
		await user.click(toggle);

		await waitFor(() => {
			expect(capturedPayload).toEqual({ circuit_breaker_enabled: "false" });
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("toggles failover on rate limit and calls mutation", async () => {
		const user = userEvent.setup();
		let capturedPayload: Record<string, string> | undefined;

		server.use(
			...mockSettings({ body: {} }),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = screen.getByRole("switch", {
			name: /failover on rate limit/i,
		});
		await user.click(toggle);

		await waitFor(() => {
			expect(capturedPayload).toEqual({ failover_on_rate_limit: "true" });
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast when toggle mutation fails", async () => {
		const user = userEvent.setup();

		server.use(
			...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			http.put("/api/settings", () =>
				HttpResponse.json({ error: "Internal Server Error" }, { status: 500 }),
			),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		const toggle = screen.getByRole("switch", {
			name: /enable circuit breaker/i,
		});
		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("toggles circuit breaker from OFF to ON and calls mutation", async () => {
		const user = userEvent.setup();
		let capturedPayload: Record<string, string> | undefined;

		server.use(
			...mockSettings({ body: { circuit_breaker_enabled: "false" } }),
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(
				screen.queryByLabelText("Failure Threshold"),
			).not.toBeInTheDocument();
			expect(
				screen.queryByLabelText("Cooldown Period"),
			).not.toBeInTheDocument();
		});

		const toggle = screen.getByRole("switch", {
			name: /enable circuit breaker/i,
		});
		await user.click(toggle);

		await waitFor(() => {
			expect(capturedPayload).toEqual({ circuit_breaker_enabled: "true" });
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	describe("per-setting reset", () => {
		it("calls api.settings.reset when reset button is clicked", async () => {
			const resetSpy = vi.spyOn(api.settings, "reset");
			resetSpy.mockResolvedValueOnce({});

			const user = userEvent.setup();
			renderWithProviders(<CircuitBreakerSettings onResetSection={() => {}} />);

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
	});
});
