import { API_BASE, fetchJSON, getAuthHeaders, getCsrfToken } from "../http";
import type {
	AlertEventDef,
	AlertStatus,
	AlertTargets,
	BackupClassification,
	BackupEntry,
	SystemStats,
} from "../types";

export const settings = {
	get: async (): Promise<Record<string, string>> => {
		return fetchJSON<Record<string, string>>(
			`${API_BASE}/api/settings`,
			{
				headers: getAuthHeaders(),
			},
			"Failed to fetch settings",
		);
	},
	update: async (
		settings: Record<string, string>,
	): Promise<Record<string, string>> => {
		return fetchJSON<Record<string, string>>(
			`${API_BASE}/api/settings`,
			{
				method: "PUT",
				headers: getAuthHeaders(),
				body: JSON.stringify(settings),
			},
			"Failed to update settings",
		);
	},
	reset: async (keys: string[] = []): Promise<Record<string, string>> => {
		return fetchJSON<Record<string, string>>(
			`${API_BASE}/api/settings`,
			{
				method: "DELETE",
				headers: getAuthHeaders(),
				body: JSON.stringify({ keys }),
			},
			"Failed to reset settings",
		);
	},
};

export const alert = {
	getEvents: async (): Promise<AlertEventDef[]> => {
		return fetchJSON<AlertEventDef[]>(
			`${API_BASE}/api/alert/events`,
			{ headers: getAuthHeaders() },
			"Failed to fetch alert events",
		);
	},
	// With no body, tests the saved configuration (the card's Send test) and
	// sends no request body at all, so no Content-Type header rides along
	// either. The setup wizard passes an explicit {api_url, targets} to test
	// values before either is saved; either field may be omitted to fall back
	// to the saved one.
	test: async (body?: {
		api_url?: string;
		targets?: string[];
	}): Promise<{ ok: boolean }> => {
		const headers = getAuthHeaders();
		if (!body) delete headers["Content-Type"];
		return fetchJSON<{ ok: boolean }>(
			`${API_BASE}/api/alert/test`,
			{
				method: "POST",
				headers,
				...(body ? { body: JSON.stringify(body) } : {}),
			},
			"Test notification failed",
		);
	},
	status: async (): Promise<AlertStatus> => {
		return fetchJSON<AlertStatus>(
			`${API_BASE}/api/alert/status`,
			{ headers: getAuthHeaders() },
			"Failed to fetch alert status",
		);
	},
	// Checks the reachability of an apprise-api URL the operator typed but has
	// not saved yet; the setup wizard gates its first step on this.
	probe: async (apiUrl: string): Promise<AlertStatus> => {
		return fetchJSON<AlertStatus>(
			`${API_BASE}/api/alert/probe`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ api_url: apiUrl }),
			},
			"Failed to probe alert URL",
		);
	},
	// The saved destinations, decrypted for the admin UI's readable list.
	targets: async (): Promise<AlertTargets> => {
		return fetchJSON<AlertTargets>(
			`${API_BASE}/api/alert/targets`,
			{ headers: getAuthHeaders() },
			"Failed to fetch alert targets",
		);
	},
};

export const version = {
	getLatest: async (options?: RequestInit): Promise<{ tag_name: string }> => {
		return fetchJSON<{ tag_name: string }>(
			`${API_BASE}/api/version/latest`,
			{ headers: getAuthHeaders(), ...options },
			"Failed to fetch latest version",
		);
	},
};

export const system = {
	get: async (): Promise<SystemStats> => {
		const now = new Date();
		const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate());
		const since = midnight.toISOString();
		return fetchJSON<SystemStats>(
			`${API_BASE}/api/system?since=${encodeURIComponent(since)}`,
			{
				headers: getAuthHeaders(),
			},
			"Failed to fetch system stats",
		);
	},
};

export const backups = {
	list: async (): Promise<BackupEntry[]> => {
		return fetchJSON<BackupEntry[]>(
			`${API_BASE}/api/backups`,
			{
				headers: getAuthHeaders(),
			},
			"Failed to fetch backups",
		);
	},
	create: async (): Promise<BackupEntry> => {
		return fetchJSON<BackupEntry>(
			`${API_BASE}/api/backups`,
			{
				method: "POST",
				headers: getAuthHeaders(),
			},
			"Failed to create backup",
		);
	},
	downloadUrl: (filename: string): string => {
		return `${API_BASE}/api/backups/${encodeURIComponent(filename)}`;
	},
	/** The backup's signature sidecar contents, for pasting into the restore
	 *  form. 404 (thrown) for an unsigned backup. */
	signature: async (filename: string): Promise<{ signature: string }> => {
		return fetchJSON<{ signature: string }>(
			`${API_BASE}/api/backups/${encodeURIComponent(filename)}/signature`,
			{ headers: getAuthHeaders() },
			"Failed to fetch backup signature",
		);
	},
	delete: async (filename: string): Promise<void> => {
		const response = await fetch(
			`${API_BASE}/api/backups/${encodeURIComponent(filename)}`,
			{
				method: "DELETE",
				headers: getAuthHeaders(),
			},
		);
		if (!response.ok) {
			throw new Error("Failed to delete backup");
		}
	},
	restore: async (
		file: File,
		adminToken: string,
		signature = "",
	): Promise<{ migration_count: number; known_count: number }> => {
		const formData = new FormData();
		formData.append("dump", file);
		formData.append("admin_token", adminToken);
		// The dump's .sig sidecar contents, when the operator has them. The
		// server rejects a mismatch outright and records a restore without one
		// as unverified. It treats a blank value as absent itself; omitting the
		// field here just keeps the request honest about what was supplied.
		if (signature.trim()) {
			formData.append("signature", signature.trim());
		}

		// Must not set Content-Type: the browser needs to auto-set
		// multipart/form-data with the correct boundary for FormData. The
		// session rides in the cookie; the CSRF token authorizes this write.
		const csrf = getCsrfToken();
		const response = await fetch(`${API_BASE}/api/backups/restore`, {
			method: "POST",
			credentials: "same-origin",
			headers: csrf ? { "X-CSRF-Token": csrf } : {},
			body: formData,
		});
		if (!response.ok) {
			const text = await response.text();
			throw new Error(`Restore failed: ${response.status} ${text}`);
		}
		return response.json();
	},
	prunePreview: async (): Promise<BackupClassification> => {
		return fetchJSON<BackupClassification>(
			`${API_BASE}/api/backups/prune-preview`,
			{
				method: "POST",
				headers: getAuthHeaders(),
			},
			"Failed to preview backup pruning",
		);
	},
	prune: async (): Promise<BackupClassification> => {
		return fetchJSON<BackupClassification>(
			`${API_BASE}/api/backups/prune`,
			{
				method: "POST",
				headers: getAuthHeaders(),
			},
			"Failed to prune backups",
		);
	},
};
