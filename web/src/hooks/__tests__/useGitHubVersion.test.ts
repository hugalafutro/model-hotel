import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockSettings, mockVersionLatest } from "../../test/helpers";
import { server } from "../../test/mocks/server";
import { useGitHubVersion } from "../useGitHubVersion";

const STORAGE_KEY = "github-latest-version";

describe("useGitHubVersion", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.restoreAllMocks();
	});

	it("returns dev running version when settings fetch fails", async () => {
		server.use(...mockSettings({ status: 500 }));
		server.use(...mockVersionLatest({ status: 500 }));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.running).toBe("dev");
		expect(result.current.latest).toBe("GitHub");
		expect(result.current.updateAvailable).toBe(false);
	});

	it("returns running version from settings API", async () => {
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		server.use(...mockVersionLatest({ status: 500 }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("v1.0.0");
	});

	it("returns cached latest version from localStorage on mount", () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		server.use(...mockVersionLatest({ status: 500 }));
		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.latest).toBe("v0.1.2");
	});

	it("skips fetch when cache is fresh", async () => {
		const cached = JSON.stringify({ tag: "v0.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		const fetchSpy = vi.spyOn(globalThis, "fetch");
		renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		// Should not call /api/version/latest when cache is fresh
		expect(fetchSpy).not.toHaveBeenCalledWith(
			expect.stringContaining("/api/version/latest"),
			expect.anything(),
		);
	});

	it("fetches when cache is stale", async () => {
		const staleTimestamp = Date.now() - 31 * 60 * 1000;
		const cached = JSON.stringify({ tag: "v0.1.1", timestamp: staleTimestamp });
		localStorage.setItem(STORAGE_KEY, cached);

		server.use(...mockVersionLatest({ body: { tag_name: "v0.2" } }));

		renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});
	});

	it("updates latest from API response", async () => {
		server.use(...mockVersionLatest({ body: { tag_name: "v0.2" } }));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.latest).toBe("GitHub");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.2");
	});

	it("caches fetched version in localStorage", async () => {
		server.use(...mockVersionLatest({ body: { tag_name: "v0.2" } }));

		renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		const raw = localStorage.getItem(STORAGE_KEY);
		const stored = JSON.parse(raw ?? "");
		expect(stored.tag).toBe("v0.2");
		expect(stored.timestamp).toBeTypeOf("number");
	});

	it("keeps current value on HTTP error", async () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		// Make cache stale so it tries to fetch
		const staleTimestamp = Date.now() - 31 * 60 * 1000;
		localStorage.setItem(
			STORAGE_KEY,
			JSON.stringify({ tag: "v0.1.2", timestamp: staleTimestamp }),
		);

		server.use(...mockVersionLatest({ status: 403 }));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.latest).toBe("v0.1.2");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.1.2");
	});

	it("keeps current value on network error", async () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		// Make cache stale
		const staleTimestamp = Date.now() - 31 * 60 * 1000;
		localStorage.setItem(
			STORAGE_KEY,
			JSON.stringify({ tag: "v0.1.2", timestamp: staleTimestamp }),
		);

		server.use(...mockVersionLatest({ status: 500 }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.1.2");
	});

	it("ignores response without tag_name", async () => {
		server.use(...mockVersionLatest({ body: { message: "Not Found" } }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("GitHub");
	});

	it("sets updateAvailable=true when latest > running", async () => {
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		server.use(...mockVersionLatest({ body: { tag_name: "v1.1.0" } }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("v1.0.0");
		expect(result.current.latest).toBe("v1.1.0");
		expect(result.current.updateAvailable).toBe(true);
	});

	it("sets updateAvailable=false when versions match", async () => {
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		server.use(...mockVersionLatest({ body: { tag_name: "v1.0.0" } }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.updateAvailable).toBe(false);
	});

	it("sets updateAvailable=true when running is dev", async () => {
		server.use(...mockSettings({ body: { app_version: "dev" } }));
		server.use(...mockVersionLatest({ body: { tag_name: "v1.0.0" } }));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("dev");
		expect(result.current.updateAvailable).toBe(true);
	});
});
