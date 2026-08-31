import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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
		const icon = document.querySelector(".icon-shield");
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

	it("disables threshold and cooldown when circuit breaker is off", async () => {
		server.use(
			...mockSettings({
				body: { circuit_breaker_enabled: "false" },
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);
		await waitFor(() => {
			expect(screen.getByLabelText("Failure Threshold")).toBeDisabled();
			expect(screen.getByLabelText("Cooldown Period")).toBeDisabled();
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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

	// The span slider is looked up by element id rather than by its label, so the
	// assertions hold whatever locale the suite runs under.
	function spanSlider(): HTMLInputElement | null {
		return document.querySelector<HTMLInputElement>(
			"#circuit-breaker-span-models",
		);
	}

	it("shows the model span with the breaker's own default and bounds", async () => {
		// An unset key means the breaker is running its own default of 2, so the
		// slider has to show that rather than a zero nothing obeys. The bounds
		// mirror the floor the breaker enforces (1 reproduces the old
		// provider-wide behaviour) and a ceiling above any real catalog.
		server.use(...mockSettings({ body: { circuit_breaker_enabled: "true" } }));
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(spanSlider()).toBeInTheDocument();
		});
		expect(spanSlider()).toHaveValue("2");
		expect(spanSlider()).toHaveAttribute("min", "1");
		expect(spanSlider()).toHaveAttribute("max", "100");
	});

	it("shows the stored model span and writes the key the breaker reads", async () => {
		let written: unknown;
		server.use(
			...mockSettings({
				body: {
					circuit_breaker_enabled: "true",
					circuit_breaker_span_models: "3",
				},
			}),
			http.put("/api/settings", async ({ request }) => {
				written = await request.json();
				return HttpResponse.json({ ok: true });
			}),
		);
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(spanSlider()).toHaveValue("3");
		});

		const input = spanSlider() as HTMLInputElement;
		fireEvent.change(input, { target: { value: "5" } });
		fireEvent.pointerUp(input);

		await waitFor(() => {
			expect(written).toEqual({ circuit_breaker_span_models: "5" });
		});
	});

	it("disables the model span while the breaker itself is off", async () => {
		server.use(...mockSettings({ body: { circuit_breaker_enabled: "false" } }));
		renderWithProviders(
			<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(spanSlider()).toBeDisabled();
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
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
			expect(screen.getByLabelText("Failure Threshold")).toBeDisabled();
			expect(screen.getByLabelText("Cooldown Period")).toBeDisabled();
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

	describe("hedging", () => {
		it("hedging toggle is OFF by default and hides the delay slider and notice", async () => {
			server.use(...mockSettings({ body: {} }));
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(
					screen.getByRole("switch", { name: /hedge slow streams/i }),
				).toHaveAttribute("aria-checked", "false");
			});
			expect(screen.getByLabelText("Hedge Delay")).toBeDisabled();
			expect(
				screen.queryByText(/Hedging fires a second/i),
			).not.toBeInTheDocument();
		});

		it("shows the delay slider and cost notice when hedging is enabled", async () => {
			server.use(
				...mockSettings({
					body: { hedging_enabled: "true", hedge_delay: "4s" },
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				const input = screen.getByLabelText("Hedge Delay") as HTMLInputElement;
				expect(input.value).toBe("4");
				expect(input.min).toBe("1");
				expect(input.max).toBe("15");
			});
			expect(
				await screen.findByText(/Hedging fires a second/i),
			).toBeInTheDocument();
			// The notice is a semantic warning callout stacked under the Hedging
			// group, in the Hedging column.
			const notice = screen.getByTestId("hedging-notice");
			expect(notice).toHaveClass("ui-callout", "ui-callout-warning");
			const column = screen.getByTestId("hedging-column");
			expect(column).toContainElement(notice);
			expect(
				within(column).getByRole("switch", { name: /hedge slow streams/i }),
			).toBeInTheDocument();
			expect(
				within(column).queryByRole("switch", {
					name: /enable circuit breaker/i,
				}),
			).not.toBeInTheDocument();
		});

		it("toggles hedging on and calls mutation", async () => {
			const user = userEvent.setup();
			let capturedPayload: Record<string, string> | undefined;

			server.use(
				...mockSettings({ body: {} }),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json({ ok: true });
				}),
			);

			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);

			const toggle = screen.getByRole("switch", {
				name: /hedge slow streams/i,
			});
			await user.click(toggle);

			await waitFor(() => {
				expect(capturedPayload).toEqual({ hedging_enabled: "true" });
				expect(screen.getByText("Settings saved")).toBeInTheDocument();
			});
		});

		it("calls mutation when the hedge delay slider changes", async () => {
			let mutationCalled = false;

			server.use(
				...mockSettings({
					body: { hedging_enabled: "true", hedge_delay: "4s" },
				}),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					const body = await request.json();
					if (
						typeof body === "object" &&
						body !== null &&
						"hedge_delay" in body
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
				expect(screen.getByLabelText("Hedge Delay")).toBeInTheDocument();
			});

			const input = screen.getByLabelText("Hedge Delay") as HTMLInputElement;
			fireEvent.change(input, { target: { value: "8" } });
			fireEvent.pointerUp(input);

			await waitFor(() => {
				expect(mutationCalled).toBe(true);
			});
		});
	});

	// Addressed by data-testid and element id like the quota-pin controls, so
	// the tests are locale-independent.
	describe("probe backoff", () => {
		const backoffToggle = () =>
			screen
				.getByTestId("backoff-row")
				.querySelector("button[role='switch']") as HTMLButtonElement;
		const backoffMaxSlider = () =>
			document.getElementById(
				"circuit-breaker-backoff-max",
			) as HTMLInputElement;
		const backoffMaxNumberBox = () =>
			backoffMaxSlider().parentElement?.querySelector(
				"input[type='number']",
			) as HTMLInputElement;

		it("renders the backoff toggle on when circuit_breaker_backoff_enabled is absent, matching the backend default of true", async () => {
			server.use(
				...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffToggle()).toHaveAttribute("aria-checked", "true");
			});
		});

		it("sends circuit_breaker_backoff_enabled=false when the backoff toggle is switched off", async () => {
			const user = userEvent.setup();
			let capturedPayload: Record<string, string> | undefined;

			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_enabled: "true",
					},
				}),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json({ ok: true });
				}),
			);

			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);

			await waitFor(() => {
				expect(backoffToggle()).toHaveAttribute("aria-checked", "true");
			});
			await user.click(backoffToggle());

			await waitFor(() => {
				expect(capturedPayload).toEqual({
					circuit_breaker_backoff_enabled: "false",
				});
			});
		});

		it("renders the backoff ceiling as 60 minutes when circuit_breaker_backoff_max is absent or stored as a non-positive duration", async () => {
			server.use(
				...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			);
			const absent = renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffMaxSlider().value).toBe("60");
			});
			absent.unmount();

			// The breaker reads a non-positive ceiling as unset and applies 1h, so
			// the slider must show the ceiling actually in force.
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_max: "0s",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffMaxSlider().value).toBe("60");
			});
		});

		it("clamps a stored circuit_breaker_backoff_max above the slider maximum so both halves of the control agree", async () => {
			let putCalled = false;
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_max: "48h0m0s",
					},
				}),
				http.put("/api/settings", () => {
					putCalled = true;
					return HttpResponse.json({ ok: true });
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffMaxSlider().value).toBe("1440");
			});
			expect(backoffMaxNumberBox().value).toBe("1440");
			// Clamping is display only: rendering must never write storage back.
			expect(putCalled).toBe(false);
		});

		it("converts a stored circuit_breaker_backoff_max Go duration into slider minutes", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_max: "3h0m0s",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffMaxSlider().value).toBe("180");
			});
		});

		it("sends circuit_breaker_backoff_max as a Go duration string when the ceiling slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_max: "1h0m0s",
					},
				}),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json({ ok: true });
				}),
			);

			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);

			await waitFor(() => {
				expect(backoffMaxSlider().value).toBe("60");
			});

			const slider = backoffMaxSlider();
			fireEvent.change(slider, { target: { value: "90" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({
					circuit_breaker_backoff_max: "1h30m",
				});
			});
		});

		it("disables the backoff ceiling slider while circuit_breaker_backoff_enabled is false", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_backoff_enabled: "false",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(backoffMaxSlider()).toBeDisabled();
			});
			expect(backoffToggle()).toHaveAttribute("aria-checked", "false");
			expect(backoffToggle()).not.toBeDisabled();
		});
	});

	// Locale-independent by construction: the quota-pin controls are addressed
	// by data-testid and element id, never by their translated label, so these
	// tests keep passing under every locale the suite may be run in.
	describe("quota pin", () => {
		const quotaPinToggle = () =>
			screen
				.getByTestId("quota-pin-row")
				.querySelector("button[role='switch']") as HTMLButtonElement;
		const quotaPinMaxSlider = () =>
			document.getElementById(
				"circuit-breaker-quota-pin-max",
			) as HTMLInputElement;
		// The number box beside the range track. The browser sanitizes a range
		// input's value against min/max but leaves a number input alone, so the
		// two halves only agree if the component itself clamped the stored value.
		const quotaPinMaxNumberBox = () =>
			quotaPinMaxSlider().parentElement?.querySelector(
				"input[type='number']",
			) as HTMLInputElement;

		it("renders the quota pin toggle on when circuit_breaker_quota_pin_enabled is absent, matching the backend default of true", async () => {
			server.use(
				...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinToggle()).toHaveAttribute("aria-checked", "true");
			});
		});

		it("renders the quota pin toggle off when circuit_breaker_quota_pin_enabled is stored as false", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_enabled: "false",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinToggle()).toHaveAttribute("aria-checked", "false");
			});
		});

		it("sends circuit_breaker_quota_pin_enabled=false when the quota pin toggle is switched off", async () => {
			const user = userEvent.setup();
			let capturedPayload: Record<string, string> | undefined;

			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_enabled: "true",
					},
				}),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json({ ok: true });
				}),
			);

			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);

			await waitFor(() => {
				expect(quotaPinToggle()).toHaveAttribute("aria-checked", "true");
			});
			await user.click(quotaPinToggle());

			await waitFor(() => {
				expect(capturedPayload).toEqual({
					circuit_breaker_quota_pin_enabled: "false",
				});
			});
		});

		it("renders the quota pin ceiling as 24 hours when circuit_breaker_quota_pin_max is absent", async () => {
			server.use(
				...mockSettings({ body: { circuit_breaker_enabled: "true" } }),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("24");
			});
		});

		it("shows 24 hours when circuit_breaker_quota_pin_max is stored as a non-positive duration, which the breaker reads as unset", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_max: "0s",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("24");
			});
		});

		it("clamps a stored circuit_breaker_quota_pin_max below the slider minimum so both halves of the control agree", async () => {
			let putCalled = false;
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_max: "30m",
					},
				}),
				http.put("/api/settings", () => {
					putCalled = true;
					return HttpResponse.json({ ok: true });
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("1");
			});
			expect(quotaPinMaxNumberBox().value).toBe("1");
			// Clamping is display only: rendering must never write storage back.
			expect(putCalled).toBe(false);
		});

		it("clamps a stored circuit_breaker_quota_pin_max above the slider maximum so both halves of the control agree", async () => {
			let putCalled = false;
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_max: "336h0m0s",
					},
				}),
				http.put("/api/settings", () => {
					putCalled = true;
					return HttpResponse.json({ ok: true });
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("168");
			});
			expect(quotaPinMaxNumberBox().value).toBe("168");
			expect(putCalled).toBe(false);
		});

		it("converts a stored circuit_breaker_quota_pin_max Go duration into slider hours", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_max: "72h0m0s",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("72");
			});
		});

		it("sends circuit_breaker_quota_pin_max as a Go duration string when the ceiling slider changes", async () => {
			let capturedPayload: Record<string, string> | undefined;

			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_max: "24h0m0s",
					},
				}),
				http.put("/api/settings", async ({ request }) => {
					if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					capturedPayload = (await request.json()) as Record<string, string>;
					return HttpResponse.json({ ok: true });
				}),
			);

			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);

			await waitFor(() => {
				expect(quotaPinMaxSlider().value).toBe("24");
			});

			const slider = quotaPinMaxSlider();
			fireEvent.change(slider, { target: { value: "6" } });
			fireEvent.pointerUp(slider);

			await waitFor(() => {
				expect(capturedPayload).toEqual({
					circuit_breaker_quota_pin_max: "6h",
				});
			});
		});

		it("disables the quota pin ceiling slider while circuit_breaker_quota_pin_enabled is false", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "true",
						circuit_breaker_quota_pin_enabled: "false",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinMaxSlider()).toBeDisabled();
			});
			expect(quotaPinToggle()).not.toBeDisabled();
		});

		it("disables both quota pin controls while the circuit breaker itself is off", async () => {
			server.use(
				...mockSettings({
					body: {
						circuit_breaker_enabled: "false",
						circuit_breaker_quota_pin_enabled: "true",
					},
				}),
			);
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={onToggle} />,
			);
			await waitFor(() => {
				expect(quotaPinToggle()).toBeDisabled();
			});
			expect(quotaPinMaxSlider()).toBeDisabled();
		});
	});

	describe("per-setting reset", () => {
		it("calls api.settings.reset when reset button is clicked", async () => {
			const resetSpy = vi.spyOn(api.settings, "reset");
			resetSpy.mockResolvedValueOnce({});

			const user = userEvent.setup();
			renderWithProviders(
				<CircuitBreakerSettings
					collapsed={false}
					onToggle={() => {}}
					onResetSection={() => {}}
				/>,
			);

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

		it("resets exactly the hedging keys from the Hedging column's inline reset buttons", async () => {
			const resetSpy = vi.spyOn(api.settings, "reset");
			resetSpy.mockResolvedValue({});
			server.use(
				...mockSettings({
					body: { hedging_enabled: "true", hedge_delay: "4s" },
				}),
			);
			const user = userEvent.setup();
			renderWithProviders(
				<CircuitBreakerSettings collapsed={false} onToggle={() => {}} />,
			);
			const column = await screen.findByTestId("hedging-column");
			// Two reset buttons live in the column, one per control, in DOM
			// order: the Hedge Slow Streams toggle, then the Hedge Delay slider.
			const resets = within(column).getAllByRole("button", {
				name: /reset this setting to default/i,
			});
			expect(resets).toHaveLength(2);
			await user.click(resets[0]);
			await waitFor(() =>
				expect(resetSpy).toHaveBeenLastCalledWith(["hedging_enabled"]),
			);
			await user.click(resets[1]);
			await waitFor(() =>
				expect(resetSpy).toHaveBeenLastCalledWith(["hedge_delay"]),
			);
			resetSpy.mockRestore();
		});
	});
});
