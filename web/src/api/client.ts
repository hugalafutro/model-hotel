import type { Provider, CreateProviderRequest, ProxyKey, Model, LogsResponse, Stats } from './types'

const API_BASE = '';

let adminToken: string | null = null;

export function setAdminToken(token: string) {
  adminToken = token;
}

export function getAdminToken(): string | null {
  return adminToken;
}

function getAuthHeaders(): Record<string, string> {
  const token = adminToken || localStorage.getItem('adminToken');
  if (!token) {
    throw new Error('Admin token not set');
  }
  return {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

export const api = {
  providers: {
    list: async (): Promise<Provider[]> => {
      const response = await fetch(`${API_BASE}/api/providers`, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch providers: ${response.status} ${text}`);
      }
      return response.json();
    },
    create: async (data: CreateProviderRequest): Promise<Provider> => {
      const response = await fetch(`${API_BASE}/api/providers`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(data),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to create provider: ${response.status} ${text}`);
      }
      return response.json();
    },
    delete: async (id: string): Promise<void> => {
      const response = await fetch(`${API_BASE}/api/providers/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (!response.ok && response.status !== 204) {
        throw new Error('Failed to delete provider');
      }
    },
    discover: async (id: string): Promise<{discovered: number}> => {
      const response = await fetch(`${API_BASE}/api/providers/${id}/discover`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to discover models: ${response.status} ${text}`);
      }
      return response.json();
    },
  },

  proxyKeys: {
    list: async (): Promise<ProxyKey[]> => {
      const response = await fetch(`${API_BASE}/api/keys`, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch keys: ${response.status} ${text}`);
      }
      return response.json();
    },
    create: async (name: string): Promise<ProxyKey> => {
      const response = await fetch(`${API_BASE}/api/keys`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ name }),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to create key: ${response.status} ${text}`);
      }
      return response.json();
    },
    delete: async (id: string): Promise<void> => {
      const response = await fetch(`${API_BASE}/api/keys/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (!response.ok && response.status !== 204) {
        throw new Error('Failed to delete key');
      }
    },
  },

  models: {
    list: async (providerId?: string): Promise<Model[]> => {
      const url = providerId
        ? `${API_BASE}/api/models?provider_id=${providerId}`
        : `${API_BASE}/api/models`;
      const response = await fetch(url, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch models: ${response.status} ${text}`);
      }
      return response.json();
    },
  },

  logs: {
    list: async (params: {
      page?: number;
      per_page?: number;
      model_id?: string;
      provider_id?: string;
      status_code?: number;
      from?: string;
      to?: string;
    } = {}): Promise<LogsResponse> => {
      const searchParams = new URLSearchParams();
      if (params.page) searchParams.append('page', params.page.toString());
      if (params.per_page) searchParams.append('per_page', params.per_page.toString());
      if (params.model_id) searchParams.append('model_id', params.model_id);
      if (params.provider_id) searchParams.append('provider_id', params.provider_id);
      if (params.status_code) searchParams.append('status_code', params.status_code.toString());
      if (params.from) searchParams.append('from', params.from);
      if (params.to) searchParams.append('to', params.to);

      const response = await fetch(`${API_BASE}/api/logs?${searchParams}`, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch logs: ${response.status} ${text}`);
      }
      return response.json();
    },
  },

  stats: {
    get: async (): Promise<Stats> => {
      const response = await fetch(`${API_BASE}/api/stats`, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch stats: ${response.status} ${text}`);
      }
      return response.json();
    },
  },

  settings: {
    get: async (): Promise<Record<string, string>> => {
      const response = await fetch(`${API_BASE}/api/settings`, {
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to fetch settings: ${response.status} ${text}`);
      }
      return response.json();
    },
    update: async (settings: Record<string, string>): Promise<Record<string, string>> => {
      const response = await fetch(`${API_BASE}/api/settings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(settings),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Failed to update settings: ${response.status} ${text}`);
      }
      return response.json();
    },
  },
};