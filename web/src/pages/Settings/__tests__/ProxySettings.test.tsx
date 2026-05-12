import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
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
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}
				return HttpResponse.json({});
			}),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutSelect = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLSelectElement;
			expect(requestTimeoutSelect.value).toBe("1m0s");
		});

		await waitFor(() => {
			const keyCacheTTLSelect = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLSelectElement;
			expect(keyCacheTTLSelect.value).toBe("10m0s");
		});
	});

	it("displays values from settings API", async () => {
		server.use(
			http.get("/api/settings", ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
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
			const requestTimeoutSelect = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLSelectElement;
			expect(requestTimeoutSelect.value).toBe("5m0s");
		});

		await waitFor(() => {
			const keyCacheTTLSelect = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLSelectElement;
			expect(keyCacheTTLSelect.value).toBe("30m0s");
		});
	});

	it("updates request timeout via mutation", async () => {
		const user = userEvent.setup();
		let mutationCalled = false;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
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
			const requestTimeoutSelect = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLSelectElement;
			expect(requestTimeoutSelect).toBeInTheDocument();
		});

		const requestTimeoutSelect = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLSelectElement;
		await user.selectOptions(requestTimeoutSelect, "5m0s");

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
		});
	});

	it("updates key cache TTL via mutation", async () => {
		const user = userEvent.setup();
		let mutationCalled = false;

		server.use(
			http.put("/api/settings", async ({ request }) => {
				if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
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
			const keyCacheTTLSelect = screen.getByLabelText(
				/Key Cache TTL/i,
			) as HTMLSelectElement;
			expect(keyCacheTTLSelect).toBeInTheDocument();
		});

		const keyCacheTTLSelect = screen.getByLabelText(
			/Key Cache TTL/i,
		) as HTMLSelectElement;
		await user.selectOptions(keyCacheTTLSelect, "1h0m0s");

		await waitFor(() => {
			expect(mutationCalled).toBe(true);
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
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutSelect = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLSelectElement;
			expect(requestTimeoutSelect).toBeInTheDocument();
		});

		const requestTimeoutSelect = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLSelectElement;
		await user.selectOptions(requestTimeoutSelect, "2m0s");

		await waitFor(() => {
			expect(screen.getByText("Settings saved")).toBeInTheDocument();
		});
	});

	it("shows error toast on mutation failure", async () => {
		const user = userEvent.setup();

		server.use(
			http.put("/api/settings", () => HttpResponse.error("Network error")),
		);

		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			const requestTimeoutSelect = screen.getByLabelText(
				/Request Timeout/i,
			) as HTMLSelectElement;
			expect(requestTimeoutSelect).toBeInTheDocument();
		});

		const requestTimeoutSelect = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLSelectElement;
		await user.selectOptions(requestTimeoutSelect, "2m0s");

		await waitFor(() => {
			expect(screen.getByText(/Failed to save:/i)).toBeInTheDocument();
		});
	});

	it("renders all timeout options", () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		const requestTimeoutSelect = screen.getByLabelText(
			/Request Timeout/i,
		) as HTMLSelectElement;

		expect(requestTimeoutSelect.options).toHaveLength(5);
		expect(requestTimeoutSelect.options[0].value).toBe("30s");
		expect(requestTimeoutSelect.options[1].value).toBe("1m0s");
		expect(requestTimeoutSelect.options[2].value).toBe("2m0s");
		expect(requestTimeoutSelect.options[3].value).toBe("5m0s");
		expect(requestTimeoutSelect.options[4].value).toBe("10m0s");
	});

	it("renders all key cache TTL options", () => {
		renderWithProviders(
			<ProxySettings collapsed={false} onToggle={() => {}} />,
		);

		const keyCacheTTLSelect = screen.getByLabelText(
			/Key Cache TTL/i,
		) as HTMLSelectElement;

		expect(keyCacheTTLSelect.options).toHaveLength(5);
		expect(keyCacheTTLSelect.options[0].value).toBe("1m0s");
		expect(keyCacheTTLSelect.options[1].value).toBe("5m0s");
		expect(keyCacheTTLSelect.options[2].value).toBe("10m0s");
		expect(keyCacheTTLSelect.options[3].value).toBe("30m0s");
		expect(keyCacheTTLSelect.options[4].value).toBe("1h0m0s");
	});
});
