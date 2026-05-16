import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockSettings } from "../../test/helpers";
import { server } from "../../test/mocks/server";
import { useGitHubVersion } from "../useGitHubVersion";

const STORAGE_KEY = "github-latest-version";
const REPO_API =
	"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest";

/** Mock fetch only for the GitHub releases API; let MSW handle /api/* */
function mockGitHubFetch(
	impl: (url: string, init?: RequestInit) => Promise<Response>,
) {
	const realFetch = globalThis.fetch;
	vi.spyOn(globalThis, "fetch").mockImplementation((url, init) => {
		if (typeof url === "string" && url.startsWith(REPO_API)) {
			return impl(url, init);
		}
		// Fall through to real fetch (MSW intercepts /api/* requests)
		return realFetch(url, init);
	});
}

describe("useGitHubVersion", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.restoreAllMocks();
	});

	it("returns dev running version when settings fetch fails", async () => {
		server.use(...mockSettings({ status: 500 }));
		mockGitHubFetch(() => Promise.reject(new Error("offline")));

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.running).toBe("dev");
		expect(result.current.latest).toBe("GitHub");
		expect(result.current.updateAvailable).toBe(false);
	});

	it("returns running version from settings API", async () => {
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		mockGitHubFetch(() => Promise.reject(new Error("offline")));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("v1.0.0");
	});

	it("returns cached latest version from localStorage on mount", () => {
		const cached = JSON.stringify({ tag: "v0.1.2", timestamp: Date.now() });
		localStorage.setItem(STORAGE_KEY, cached);

		mockGitHubFetch(() => Promise.reject(new Error("offline")));
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

		expect(fetchSpy).not.toHaveBeenCalledWith(REPO_API, expect.anything());
	});

	it("fetches when cache is stale", async () => {
		const staleTimestamp = Date.now() - 31 * 60 * 1000;
		const cached = JSON.stringify({ tag: "v0.1.1", timestamp: staleTimestamp });
		localStorage.setItem(STORAGE_KEY, cached);

		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v0.2" }))),
		);

		renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});
	});

	it("updates latest from API response", async () => {
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v0.2" }))),
		);

		const { result } = renderHook(() => useGitHubVersion());
		expect(result.current.latest).toBe("GitHub");

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.2");
	});

	it("caches fetched version in localStorage", async () => {
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v0.2" }))),
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

		mockGitHubFetch(() =>
			Promise.resolve(new Response("rate limited", { status: 403 })),
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

		mockGitHubFetch(() => Promise.reject(new Error("network error")));

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("v0.1.2");
	});

	it("ignores response without tag_name", async () => {
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ message: "Not Found" }))),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.latest).toBe("GitHub");
	});

	it("sets updateAvailable=true when latest > running", async () => {
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v1.1.0" }))),
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
		server.use(...mockSettings({ body: { app_version: "v1.0.0" } }));
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v1.0.0" }))),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.updateAvailable).toBe(false);
	});

	it("sets updateAvailable=true when running is dev", async () => {
		server.use(...mockSettings({ body: { app_version: "dev" } }));
		mockGitHubFetch(() =>
			Promise.resolve(new Response(JSON.stringify({ tag_name: "v1.0.0" }))),
		);

		const { result } = renderHook(() => useGitHubVersion());

		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});

		expect(result.current.running).toBe("dev");
		expect(result.current.updateAvailable).toBe(true);
	});
});
