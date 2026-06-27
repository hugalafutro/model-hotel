import { useEffect, useState } from "react";
import { api } from "../api/client";

const STORAGE_KEY = "github-latest-version";
const CACHE_TTL = 30 * 60 * 1000; // 30 minutes

interface CachedVersion {
	tag: string;
	timestamp: number;
}

export interface VersionInfo {
	/** Latest release tag from GitHub (e.g. "v1.2.3") */
	latest: string;
	/** Running instance version from /api/settings app_version (e.g. "v1.0.0" or "dev") */
	running: string;
	/** Source commit SHA the running build was stamped with, or "" if unknown */
	commit: string;
	/** True when latest > running (both are semver-like tags) */
	updateAvailable: boolean;
}

function compareSemverTags(latest: string, running: string): boolean {
	const strip = (v: string) => v.replace(/^v/, "");
	const l = strip(latest);
	const r = strip(running);
	// If running is "dev" or non-semver, always consider update available
	if (!r.match(/^\d+/)) return true;
	if (!l.match(/^\d+/)) return false;
	const lp = l.split(".").map(Number);
	const rp = r.split(".").map(Number);
	for (let i = 0; i < Math.max(lp.length, rp.length); i++) {
		const a = lp[i] ?? 0;
		const b = rp[i] ?? 0;
		if (a > b) return true;
		if (a < b) return false;
	}
	return false; // equal
}

/**
 * Fetches the running app version from /api/settings and the latest
 * GitHub release tag with localStorage caching.
 */
export function useGitHubVersion(): VersionInfo {
	const [latest, setLatest] = useState<string>(() => {
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

	const [running, setRunning] = useState<string>("dev");
	const [commit, setCommit] = useState<string>("");

	// Fetch running version from settings API once
	useEffect(() => {
		let cancelled = false;
		api.settings
			.get()
			.then((settings) => {
				if (cancelled) return;
				if (settings.app_version) {
					setRunning(settings.app_version);
				}
				// "unknown" is the un-stamped sentinel; treat it as no commit.
				if (settings.app_commit && settings.app_commit !== "unknown") {
					setCommit(settings.app_commit);
				}
			})
			.catch(() => {
				/* ignore — keep default */
			});
		return () => {
			cancelled = true;
		};
	}, []);

	// Fetch latest GitHub release via backend proxy
	useEffect(() => {
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

		const controller = new AbortController();
		api.version
			.getLatest({ signal: controller.signal })
			.then((data) => {
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
					setLatest(data.tag_name);
				}
			})
			.catch(() => {
				/* network error or aborted — keep current value */
			});
		return () => {
			controller.abort();
		};
	}, []);

	const updateAvailable =
		latest !== "GitHub" && compareSemverTags(latest, running);

	return { latest, running, commit, updateAvailable };
}
