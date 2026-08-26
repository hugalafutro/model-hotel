import { API_BASE, fetchJSON, fetchOK, getAuthHeaders } from "../http";
import type { DashboardUser, UserUpsertRequest, VirtualKey } from "../types";

export const virtualKeys = {
	list: async (): Promise<VirtualKey[]> => {
		return fetchJSON<VirtualKey[]>(
			`${API_BASE}/api/virtual-keys`,
			{
				headers: getAuthHeaders(),
			},
			"Failed to fetch virtual keys",
		);
	},
	create: async (
		name: string,
		rate_limit_rps?: number | null,
		rate_limit_burst?: number | null,
		rate_limit_tpm?: number | null,
		allowed_providers?: string[] | null,
		strip_reasoning?: boolean,
		owner_user_id?: string | null,
	): Promise<VirtualKey> => {
		return fetchJSON<VirtualKey>(
			`${API_BASE}/api/virtual-keys`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({
					name,
					rate_limit_rps,
					rate_limit_burst,
					rate_limit_tpm,
					allowed_providers,
					strip_reasoning,
					owner_user_id,
				}),
			},
			"Failed to create virtual key",
		);
	},
	get: async (id: string): Promise<VirtualKey> => {
		return fetchJSON<VirtualKey>(
			`${API_BASE}/api/virtual-keys/${id}`,
			{
				headers: getAuthHeaders(),
			},
			"Failed to fetch virtual key",
		);
	},
	update: async (
		id: string,
		data: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
			rate_limit_tpm?: number | null;
			allowed_providers?: string[] | null;
			strip_reasoning?: boolean;
			// Omit to preserve the current owner; null clears it (admin only).
			owner_user_id?: string | null;
		},
	): Promise<VirtualKey> => {
		return fetchJSON<VirtualKey>(
			`${API_BASE}/api/virtual-keys/${id}`,
			{
				method: "PUT",
				headers: getAuthHeaders(),
				body: JSON.stringify(data),
			},
			"Failed to update virtual key",
		);
	},
	delete: async (id: string): Promise<void> => {
		const response = await fetch(`${API_BASE}/api/virtual-keys/${id}`, {
			method: "DELETE",
			headers: getAuthHeaders(),
		});
		if (!response.ok) {
			throw new Error("Failed to delete virtual key");
		}
	},
};

// Admin-only user management.
export const users = {
	list: async (): Promise<DashboardUser[]> =>
		fetchJSON<DashboardUser[]>(`${API_BASE}/api/users`, {
			headers: getAuthHeaders(),
		}),
	grants: async (): Promise<{ grants: string[] }> =>
		fetchJSON<{ grants: string[] }>(`${API_BASE}/api/users/grants`, {
			headers: getAuthHeaders(),
		}),
	create: async (req: UserUpsertRequest): Promise<DashboardUser> =>
		fetchJSON<DashboardUser>(
			`${API_BASE}/api/users`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify(req),
			},
			"Failed to create user",
		),
	update: async (id: string, req: UserUpsertRequest): Promise<DashboardUser> =>
		fetchJSON<DashboardUser>(
			`${API_BASE}/api/users/${id}`,
			{
				method: "PUT",
				headers: getAuthHeaders(),
				body: JSON.stringify(req),
			},
			"Failed to update user",
		),
	setPassword: async (id: string, password: string): Promise<void> => {
		await fetchJSON<{ ok: boolean }>(
			`${API_BASE}/api/users/${id}/password`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ password }),
			},
			"Failed to set password",
		);
	},
	remove: async (id: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/users/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Failed to delete user",
		);
	},
	resetTotp: async (id: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/users/${id}/totp/reset`,
			{ method: "POST", headers: getAuthHeaders() },
			"Failed to reset TOTP",
		);
	},
};
