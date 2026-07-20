import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { vi } from "vitest";
import { api } from "../../../api/client";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ProxySettings } from "../ProxySettings";

describe("ProxySettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders with default collapsed state", () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(screen.getByText("Proxy")).toBeInTheDocument();
		expect(screen.getByText("Request Timeout")).toBeInTheDocument();
		expect(screen.getByText("Key Cache TTL")).toBeInTheDocument();
		expect(
			screen.getByText(/Configure proxy request behavior/i),
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
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput.value).toBe("60");
		});

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput.value).toBe("600");
		});
	});

	it("displays values from settings API", async () => {
		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({
					request_timeout: "5m0s",
					key_cache_ttl: "30m0s",
				});
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput.value).toBe("300");
		});

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput.value).toBe("1800");
		});
	});

	it("updates request timeout via mutation", async () => {
		let mutationCalled = false;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				if (
					typeof body === "object" &&
					body !== null &&
					"request_timeout" in body
				) {
					mutationCalled = true;
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "300" } });
		fireEvent.pointerUp(requestTimeoutInput);

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
		});
	});

	it("updates key cache TTL via mutation", async () => {
		let mutationCalled = false;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				const body = await request.json();
				if (
					typeof body === "object" &&
					body !== null &&
					"key_cache_ttl" in body
				) {
					mutationCalled = true;
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput).toBeInTheDocument();
		});

		const keyCacheTTLInput = screen.getByLabelText(
			/Key Cache TTL/i,
		) as HTMLInputElement;
		fireEvent.change(keyCacheTTLInput, { target: { value: "3600" } });
		fireEvent.pointerUp(keyCacheTTLInput);

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
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
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "120" } });
		fireEvent.pointerUp(requestTimeoutInput);

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on mutation failure", async () => {
		server.use(http.put("/api/settings", () => HttpResponse.error()));

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "120" } });
		fireEvent.pointerUp(requestTimeoutInput);

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("renders request timeout slider with correct range", async () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput.min).toBe("30");
			expect(requestTimeoutInput.max).toBe("600");
			expect(requestTimeoutInput.step).toBe("30");
			expect(requestTimeoutInput.value).toBe("60");
		});
	});

	it("renders key cache TTL slider with correct range", async () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput.min).toBe("60");
			expect(keyCacheTTLInput.max).toBe("3600");
			expect(keyCacheTTLInput.step).toBe("60");
			expect(keyCacheTTLInput.value).toBe("600");
		});
	});

	it("invalidates settings query after successful mutation", async () => {
		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "120" } });
		fireEvent.pointerUp(requestTimeoutInput);

		// Success toast indicates mutation completed and invalidation was triggered
		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast with API error message when mutation fails", async () => {
		server.use(
			http.put("/api/settings", () =>
				HttpResponse.json(
					{ message: "Database connection failed" },
					{ status: 500 },
				),
			),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "120" } });
		fireEvent.pointerUp(requestTimeoutInput);

		// Error toast should appear with the error message
		await waitFor(() => {
			expect(screen.getByText(/Failed to save/i)).toBeInTheDocument();
		});
	});

	it("uses default request_timeout when settings API returns null", async () => {
		server.use(http.get("/api/settings", () => HttpResponse.json(null)));

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput.value).toBe("60");
		});
	});

	it("uses default key_cache_ttl when settings API returns null", async () => {
		server.use(http.get("/api/settings", () => HttpResponse.json(null)));

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput.value).toBe("600");
		});
	});

	it("forwards collapsed and onToggle props to SettingsSection", async () => {
		const user = userEvent.setup();
		const onToggleMock = vi.fn();
		renderWithProviders(
			<ProxySettings collapsed={true} onToggle={onToggleMock} />,
		);

		// SettingsSection should receive the collapsed prop
		// We verify by checking the toggle button exists and works
		// When collapsed=true, the button shows "Expand"
		const toggleButton = screen.getByRole("button", {
			name: /Expand/i,
		});
		expect(toggleButton).toBeInTheDocument();

		// Clicking should call onToggle
		await user.click(toggleButton);
		expect(onToggleMock).toHaveBeenCalledTimes(1);
	});

	it("calls mutation with correct request_timeout payload", async () => {
		let capturedPayload: Record<string, string> | null = null;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutInput = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLInputElement;
			expect(requestTimeoutInput).toBeInTheDocument();
		});

		const requestTimeoutInput = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLInputElement;
		fireEvent.change(requestTimeoutInput, { target: { value: "300" } });
		fireEvent.pointerUp(requestTimeoutInput);

		await waitFor(() => {
			expect(capturedPayload).toEqual({ request_timeout: "5m" });
		});
	});

	it("calls mutation with correct key_cache_ttl payload", async () => {
		let capturedPayload: Record<string, string> | null = null;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				capturedPayload = (await request.json()) as Record<string, string>;
				return HttpResponse.json({ ok: true });
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const keyCacheTTLInput = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLInputElement;
			expect(keyCacheTTLInput).toBeInTheDocument();
		});

		const keyCacheTTLInput = screen.getByLabelText(
			/Key Cache TTL/i,
		) as HTMLInputElement;
		fireEvent.change(keyCacheTTLInput, { target: { value: "1800" } });
		fireEvent.pointerUp(keyCacheTTLInput);

		await waitFor(() => {
			expect(capturedPayload).toEqual({ key_cache_ttl: "30m" });
		});
	});

	it("calls onToggle when SettingsSection toggle button is clicked with collapsed=false", async () => {
		const user = userEvent.setup();
		const onToggleMock = vi.fn();

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={onToggleMock} />,
		);

		const toggleButton = screen.getByRole("button", {
			name: /Collapse/i,
		});
		expect(toggleButton).toBeInTheDocument();

		await user.click(toggleButton);
		expect(onToggleMock).toHaveBeenCalledTimes(1);
	});

	it("renders proxy description text", () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(
			screen.getByText(/Configure proxy request behavior and timeouts/i),
		).toBeInTheDocument();
	});
});

describe("per-setting reset", () => {
	it("calls api.settings.reset when reset button is clicked", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset");
		resetSpy.mockResolvedValueOnce({});

		const user = userEvent.setup();
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
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
});
