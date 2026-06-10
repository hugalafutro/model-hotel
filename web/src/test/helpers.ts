/**
 * Test helpers for reducing MSW boilerplate and encouraging accessible query patterns.
 *
 * Usage in test files:
 *   import { mockProviders, mockModels, overrideHandlers } from "../test/helpers";
 *
 * Instead of:
 *   server.use(http.get("/api/providers", () => HttpResponse.json([...mockProviders])))
 *
 * Write:
 *   server.use(...mockProviders())
 *
 * For custom responses:
 *   server.use(...mockProviders({ status: 500 }))
 */

import { screen } from "@testing-library/react";
import { HttpResponse, http, type RequestHandler } from "msw";
import type {
	BackupEntry,
	FailoverGroup,
	LogsResponse,
	Model,
	Provider,
	Stats,
	SystemStats,
	VirtualKey,
} from "../api/types";
import {
	mockStats as defaultStats,
	mockSystemStats as defaultSystemStats,
	mockBackupEntry,
	mockFailoverGroup,
	mockModel,
	mockProvider,
	mockVirtualKey,
} from "./mocks/data";

// ── Override factories ───────────────────────────────────────────────────

interface OverrideOptions {
	/** HTTP status code (default: 200) */
	status?: number;
	/** Custom body (overrides default mock data) */
	body?: unknown;
}

/** Create handlers that return a list of providers. */
export function mockProviders(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? [mockProvider];
	return [
		http.get("/api/providers", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handlers that return a list of models. */
export function mockModels(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? [mockModel];
	return [
		http.get("/api/models", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handlers that return a list of virtual keys. */
export function mockVirtualKeys(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? [mockVirtualKey];
	return [
		http.get("/api/virtual-keys", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handlers that return failover groups. */
export function mockFailoverGroups(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {
		groups: [mockFailoverGroup],
		last_synced_at: null,
	};
	return [
		http.get("/api/failover-groups", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handlers that return backups. */
export function mockBackups(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? [mockBackupEntry];
	return [
		http.get("/api/backups", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns stats. */
export function mockStats(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? defaultStats;
	return [
		http.get("/api/stats", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns system stats. */
export function mockSystemStats(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? defaultSystemStats;
	return [
		http.get("/api/system", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns settings. */
export function mockSettings(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {};
	return [
		http.get("/api/settings", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns the latest version. */
export function mockVersionLatest(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? { tag_name: "v0.0.0" };
	return [
		http.get("/api/version/latest", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns empty logs. */
export function mockLogs(options: OverrideOptions = {}): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {
		entries: [],
		total: 0,
		page: 1,
		per_page: 25,
	};
	return [
		http.get("/api/logs", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns cursor-paginated logs. */
export function mockLogsCursor(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {
		entries: [],
		total: 0,
		has_before: false,
		has_after: false,
	};
	return [
		http.get("/api/logs/cursor", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns cursor-paginated app logs. */
export function mockAppLogsCursor(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {
		entries: [],
		total: 0,
		has_before: false,
		has_after: false,
		level_counts: {},
		source_counts: {},
	};
	return [
		http.get("/api/logs/app/cursor", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

/** Create handler that returns cursor-paginated models. */
export function mockModelsCursor(
	options: OverrideOptions = {},
): RequestHandler[] {
	const { status = 200, body } = options;
	const data = body ?? {
		entries: [],
		total: 0,
		has_before: false,
		has_after: false,
	};
	return [
		http.get("/api/models/cursor", () =>
			status === 200
				? HttpResponse.json(data)
				: HttpResponse.json(data, { status }),
		),
	];
}

// ── Streaming SSE helpers ─────────────────────────────────────────────────

/**
 * Create a ReadableStream that emits SSE-formatted data chunks.
 * Each item in `chunks` is JSON-encoded and wrapped in `data: ...\n\n`.
 * A `[DONE]` sentinel is appended automatically unless `doneSentinel` is set to null.
 *
 * @param chunks - Array of objects to serialize as SSE data events
 * @param options.delay - Milliseconds between chunks (default: 0)
 * @param options.doneSentinel - Sentinel string to send after chunks (default: "[DONE]"), null to omit
 */
export function createSSEStream(
	chunks: unknown[],
	options: { delay?: number; doneSentinel?: string | null } = {},
): ReadableStream<Uint8Array> {
	const { delay = 0, doneSentinel = "[DONE]" } = options;
	const encoder = new TextEncoder();

	return new ReadableStream({
		async start(controller) {
			for (const chunk of chunks) {
				if (delay > 0) await new Promise((r) => setTimeout(r, delay));
				controller.enqueue(
					encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`),
				);
			}
			if (doneSentinel !== null) {
				controller.enqueue(encoder.encode(`data: ${doneSentinel}\n\n`));
			}
			controller.close();
		},
	});
}

/**
 * Create an MSW handler for POST /api/chat/chat that returns a streaming SSE response.
 *
 * @param chunks - SSE data chunks to stream
 * @param options.delay - Delay between chunks in ms
 * @param options.status - HTTP status (default: 200)
 * @param options.doneSentinel - Done sentinel (default: "[DONE]"), null to omit
 */
export function mockChatStream(
	chunks: unknown[],
	options: {
		delay?: number;
		status?: number;
		doneSentinel?: string | null;
	} = {},
): RequestHandler[] {
	const { delay = 0, status = 200, doneSentinel = "[DONE]" } = options;
	return [
		http.post("/api/chat/chat", () => {
			const stream = createSSEStream(chunks, { delay, doneSentinel });
			return new HttpResponse(stream, {
				status,
				headers: {
					"Content-Type": "text/event-stream",
					"Cache-Control": "no-cache",
				},
			});
		}),
	];
}

/**
 * Create an MSW handler for POST /api/chat/arena that returns a streaming SSE response.
 */
export function mockArenaStream(
	chunks: unknown[],
	options: {
		delay?: number;
		status?: number;
		doneSentinel?: string | null;
	} = {},
): RequestHandler[] {
	const { delay = 0, status = 200, doneSentinel = "[DONE]" } = options;
	return [
		http.post("/api/chat/arena", () => {
			const stream = createSSEStream(chunks, { delay, doneSentinel });
			return new HttpResponse(stream, {
				status,
				headers: {
					"Content-Type": "text/event-stream",
					"Cache-Control": "no-cache",
				},
			});
		}),
	];
}

/** Convenience: return all default handlers for a typical page test.
 *
 *  Catch-all handlers for /api/chat/chat and /api/chat/arena are registered
 *  as initial handlers in mocks/handlers.ts (they return 503). Since MSW is
 *  first-match-wins and server.use() prepends new handlers before initial ones,
 *  any specific handlers (e.g. mockChatStream, mockArenaStream) registered
 *  via server.use() will automatically take priority over the catch-alls. */
export function mockAllDefaults(
	overrides: Partial<{
		providers: OverrideOptions | Provider[];
		models: OverrideOptions | Model[];
		virtualKeys: OverrideOptions | VirtualKey[];
		failoverGroups: OverrideOptions | FailoverGroup[];
		backups: OverrideOptions | BackupEntry[];
		stats: OverrideOptions | Stats;
		systemStats: OverrideOptions | SystemStats;
		settings: OverrideOptions | Record<string, string>;
		logs: OverrideOptions | LogsResponse;
		versionLatest: OverrideOptions | { tag_name: string };
	}> = {},
): RequestHandler[] {
	function toOpts(v: unknown): OverrideOptions {
		if (!v) return {};
		if (typeof v === "object" && v !== null && "status" in v)
			return v as OverrideOptions;
		return { body: v };
	}

	return [
		...mockProviders(toOpts(overrides.providers)),
		...mockModels(toOpts(overrides.models)),
		...mockVirtualKeys(toOpts(overrides.virtualKeys)),
		...mockFailoverGroups(toOpts(overrides.failoverGroups)),
		...mockBackups(toOpts(overrides.backups)),
		...mockStats(toOpts(overrides.stats)),
		...mockSystemStats(toOpts(overrides.systemStats)),
		...mockSettings(toOpts(overrides.settings)),
		...mockLogs(toOpts(overrides.logs)),
		...mockVersionLatest(toOpts(overrides.versionLatest)),
	];
}

// ── Testing-library query helpers ─────────────────────────────────────────

/**
 * Query helpers that enforce accessible queries.
 * Import these instead of raw querySelector / data-testid patterns.
 *
 * PREFERRED (use these):
 *   screen.getByRole("button", { name: "Submit" })
 *   screen.getByLabelText("Email")
 *   screen.getByPlaceholderText("Enter name...")
 *
 * AVOID:
 *   screen.getByTestId("submit-btn")       ← implementation detail
 *   container.querySelector("[title=...]") ← not accessible
 *   screen.getByText("Submit")              ← fragile to label changes
 */

/**
 * Find a dialog by its accessible name (requires aria-labelledby on Modal).
 * Usage: const dialog = getByDialogName("Add Provider");
 */
export function getByDialogName(name: string): HTMLElement {
	return screen.getByRole("dialog", { name });
}

// Re-export screen for convenience so test files only need one import
export { screen } from "@testing-library/react";
