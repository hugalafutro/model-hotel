import { API_BASE, fetchJSON, fetchOK, getAuthHeaders } from "../http";
import type {
	DashboardUser,
	UserUpsertRequest,
	VirtualKey,
	VirtualKeyUpsert,
} from "../types";

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
	create: async (req: VirtualKeyUpsert): Promise<VirtualKey> => {
		return fetchJSON<VirtualKey>(
			`${API_BASE}/api/virtual-keys`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify(req),
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
	update: async (id: string, data: VirtualKeyUpsert): Promise<VirtualKey> => {
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
		await fetchOK(
			`${API_BASE}/api/virtual-keys/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Failed to delete virtual key",
		);
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
