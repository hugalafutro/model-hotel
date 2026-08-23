import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMockLogEntry, createMockLogs } from "../../test/logFixtures";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Logs } from "../Logs";

// Virtual-key filter on the request-logs page: the dropdown renders from
// GET /api/virtual-keys and its selection rides the list query as
// virtual_key_id.

const keysFixture = [
	{
		id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		name: "jcode",
		key_preview: "sk-...aa",
		tokens_used: 0,
		last_used_at: null,
		created_at: "2026-07-01T10:00:00Z",
	},
	{
		id: "11111111-2222-4333-8444-555555555555",
		name: "other-key",
		key_preview: "sk-...bb",
		tokens_used: 0,
		last_used_at: null,
		created_at: "2026-07-01T10:00:00Z",
	},
];

describe("Logs virtual-key filter", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		localStorage.setItem("requestLogsViewMode", "paginate");
	});

	it("filters the list by the selected virtual key", async () => {
		const keyedLogs = createMockLogs([
			createMockLogEntry({ model_id: "jcode-model" }),
		]);
		const allLogs = createMockLogs([
			createMockLogEntry({ model_id: "everyone-model" }),
		]);
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json(keysFixture)),
			http.get("/api/logs", ({ request }) => {
				const vk = new URL(request.url).searchParams.get("virtual_key_id");
				return HttpResponse.json(
					vk === keysFixture[0].id ? keyedLogs : allLogs,
				);
			}),
		);

		const { user } = renderWithProviders(<Logs />);

		await waitFor(() => {
			expect(screen.getByText("everyone-model")).toBeInTheDocument();
		});

		const keyButton = await screen.findByRole("button", {
			name: /Virtual key/,
		});
		await user.click(keyButton);
		await user.click(await screen.findByText("jcode"));

		await waitFor(() => {
			expect(screen.getByText("jcode-model")).toBeInTheDocument();
		});
		expect(screen.queryByText("everyone-model")).not.toBeInTheDocument();
	});

	it("forwards the selected key to the cursor fetch in scroll mode", async () => {
		localStorage.setItem("requestLogsViewMode", "scroll");
		const cursorKeys: (string | null)[] = [];
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json(keysFixture)),
			http.get("/api/logs/cursor", ({ request }) => {
				const vk = new URL(request.url).searchParams.get("virtual_key_id");
				cursorKeys.push(vk);
				return HttpResponse.json({
					entries: [
						createMockLogEntry({
							model_id:
								vk === keysFixture[0].id ? "jcode-model" : "everyone-model",
						}),
					],
					total: 1,
					has_before: false,
					has_after: false,
				});
			}),
		);

		const { user } = renderWithProviders(<Logs />);

		await waitFor(() => {
			expect(cursorKeys.length).toBeGreaterThan(0);
		});

		const keyButton = await screen.findByRole("button", {
			name: /Virtual key/,
		});
		await user.click(keyButton);
		await user.click(await screen.findByText("jcode"));

		await waitFor(() => {
			expect(cursorKeys).toContain(keysFixture[0].id);
		});
	});

	it("hides the key dropdown when the key list is empty", async () => {
		const seen = createMockLogs([
			createMockLogEntry({ model_id: "solo-model" }),
		]);
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([])),
			http.get("/api/logs", () => HttpResponse.json(seen)),
		);
		renderWithProviders(<Logs />);
		await waitFor(() => {
			expect(screen.getByText("solo-model")).toBeInTheDocument();
		});
		expect(
			screen.queryByRole("button", { name: /Virtual key/ }),
		).not.toBeInTheDocument();
	});
});
