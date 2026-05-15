import { useEffect, useState } from "react";

const STORAGE_KEY = "github-latest-version";
const REPO_API =
	"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest";
const CACHE_TTL = 30 * 60 * 1000; // 30 minutes

interface CachedVersion {
	tag: string;
	timestamp: number;
}

/**
 * Fetches the latest GitHub release tag with localStorage caching.
 * - On mount: reads cached value from localStorage for instant display
 * - Then fetches from GitHub API in background
 * - Falls back: cached tag if offline, "GitHub" if no cache at all
 */
export function useGitHubVersion(): string {
	const [version, setVersion] = useState<string>(() => {
		try {
			const raw = localStorage.getItem(STORAGE_KEY);
			if (raw) {
				const cached: CachedVersion = JSON.parse(raw);
				return cached.tag;
			}
		} catch {
			/* ignore */
		}
		return "GitHub";
	});

	useEffect(() => {
		const controller = new AbortController();

		async function fetchVersion() {
			try {
				const res = await fetch(REPO_API, { signal: controller.signal });
				if (!res.ok) return;
				const data = await res.json();
				if (data.tag_name) {
					const cached: CachedVersion = {
						tag: data.tag_name,
						timestamp: Date.now(),
					};
					try {
						localStorage.setItem(STORAGE_KEY, JSON.stringify(cached));
					} catch {
						/* quota exceeded */
					}
					setVersion(data.tag_name);
				}
			} catch {
				/* network error or aborted - keep current value */
			}
		}

		// Check if cache is still fresh
		try {
			const raw = localStorage.getItem(STORAGE_KEY);
			if (raw) {
				const cached: CachedVersion = JSON.parse(raw);
				if (Date.now() - cached.timestamp < CACHE_TTL) {
					return; // cache is fresh, skip fetch
				}
			}
		} catch {
			/* ignore, proceed to fetch */
		}

		fetchVersion();
		return () => controller.abort();
	}, []);

	return version;
}
