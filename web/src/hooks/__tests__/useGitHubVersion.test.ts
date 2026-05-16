import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useGitHubVersion } from "../useGitHubVersion";

// Mock the api module
vi.mock("../../api/client", () => ({
	api: {
		settings: {
			get: vi.fn().mockResolvedValue({ app_version: "v1.0.0" }),
		},
	},
}));

import { api } from "../../api/client";

const STORAGE_KEY = "github-latest-version";
const REPO_API =
	"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest";

describe("useGitHubVersion", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.restoreAllMocks();
		// Reset api.settings.get mock
		vi.mocked(api.settings.get).mockResolvedValue({
			app_version: "v1.0.0",
		});
	});

	it("returns dev running version when settings fetch fails", async () => {
		vi.mocked(api.settings.get).mockRejectedValue(new Error("offline"));
		vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("offline"));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.running).toBe("dev");
		expect(result.current.latest).toBe("GitHub");
		expect(result.current.updateAvailable).toBe(false);
	});

	it("returns running version from settings API", async () => {
		vi.mocked(api.settings.get).mockResolvedValue({
			app_version: "v1.0.0",
		});
		vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("offline"));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("v1.0.0");
	});

	it("returns cached latest version from localStorage on mount", () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		vi.spyOn(globalThis, "fetch");
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

		expect(fetchSpy).not.toHaveBeenCalled();
	});

	it("fetches when cache is stale", async () => {
		const staleTimestamp = Date.now() - 31 * 60 * 1000;
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

	it("updates latest from API response", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ tag_name: "v0.2" })),
		);

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.latest).toBe("GitHub");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.2");
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
		expect(result.current.latest).toBe("v0.1.2");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.1.2");
	});

	it("keeps current value on network error", async () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("network error"));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.1.2");
	});

	it("ignores response without tag_name", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ message: "Not Found" })),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("GitHub");
	});

	it("sets updateAvailable=true when latest > running", async () => {
		vi.mocked(api.settings.get).mockResolvedValue({
			app_version: "v1.0.0",
		});
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ tag_name: "v1.1.0" })),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("v1.0.0");
		expect(result.current.latest).toBe("v1.1.0");
		expect(result.current.updateAvailable).toBe(true);
	});

	it("sets updateAvailable=false when versions match", async () => {
		vi.mocked(api.settings.get).mockResolvedValue({
			app_version: "v1.0.0",
		});
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ tag_name: "v1.0.0" })),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.updateAvailable).toBe(false);
	});

	it("sets updateAvailable=true when running is dev", async () => {
		vi.mocked(api.settings.get).mockResolvedValue({
			app_version: "dev",
		});
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ tag_name: "v1.0.0" })),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("dev");
		expect(result.current.updateAvailable).toBe(true);
	});
});
