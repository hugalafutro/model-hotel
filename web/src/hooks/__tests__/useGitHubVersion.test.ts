import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useGitHubVersion } from "../useGitHubVersion";

const STORAGE_KEY = "github-latest-version";
const REPO_API =
	"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest";

describe("useGitHubVersion", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.restoreAllMocks();
	});

	it('returns "GitHub" when no cache exists and fetch fails', () => {
		vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("offline"));
		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current).toBe("GitHub");
	});

	it("returns cached version from localStorage on mount", () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		vi.spyOn(globalThis, "fetch");
		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current).toBe("v0.1.2");
	});

	it("skips fetch when cache is fresh", async () => {
		const cached = JSON.stringify({ tag: "v0.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		const fetchSpy = vi.spyOn(globalThis, "fetch");
		renderHook(() => useGitHubVersion());

		// Allow microtasks to flush
		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(fetchSpy).not.toHaveBeenCalled();
	});

	it("fetches when cache is stale", async () => {
		const staleTimestamp = Date.now() - 31 * 60 * 1000; // 31 min ago
		const cached = JSON.stringify({ tag: "v0.1.1", timestamp: staleTimestamp });
		localStorage.setItem(STORAGE_KEY, cached);

		const fetchSpy = vi
			.spyOn(globalThis, "fetch")
			.mockResolvedValue(new Response(JSON.stringify({ tag_name: "v0.2" })));

		renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(fetchSpy).toHaveBeenCalledWith(REPO_API, {
			signal: expect.any(AbortSignal),
		});
	});

	it("updates version from API response", async () => {
		const fetchSpy = vi
			.spyOn(globalThis, "fetch")
			.mockResolvedValue(new Response(JSON.stringify({ tag_name: "v0.2" })));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current).toBe("GitHub");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current).toBe("v0.2");
		expect(fetchSpy).toHaveBeenCalledWith(REPO_API, {
			signal: expect.any(AbortSignal),
		});
	});

	it("caches fetched version in localStorage", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ tag_name: "v0.2" })),
		);

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

		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response("rate limited", { status: 403 }),
		);

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current).toBe("v0.1.2");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current).toBe("v0.1.2");
	});

	it("keeps current value on network error", async () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("network error"));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current).toBe("v0.1.2");
	});

	it("ignores response without tag_name", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ message: "Not Found" })),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current).toBe("GitHub");
	});
});
