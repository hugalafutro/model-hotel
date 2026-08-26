import { API_BASE, fetchOK, getAuthHeaders } from "../http";

export const chat = {
	completions: async (body: {
		model: string;
		stream: boolean;
		messages: Array<{ role: string; content: string }>;
		temperature?: number;
		max_tokens?: number;
		top_p?: number;
		min_p?: number;
		top_k?: number;
		frequency_penalty?: number;
		presence_penalty?: number;
	}): Promise<Response> => {
		return fetchOK(
			`${API_BASE}/api/chat/completions`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify(body),
			},
			"Chat failed",
		);
	},

	chat: async (body: {
		model: string;
		stream: boolean;
		messages: Array<{ role: string; content: string }>;
		temperature?: number;
		max_tokens?: number;
		top_p?: number;
		min_p?: number;
		top_k?: number;
		frequency_penalty?: number;
		presence_penalty?: number;
		signal?: AbortSignal;
	}): Promise<Response> => {
		return fetchOK(
			`${API_BASE}/api/chat/chat`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify(body),
				...(body.signal ? { signal: body.signal } : {}),
			},
			"Chat failed",
		);
	},

	arena: async (body: {
		model: string;
		stream: boolean;
		messages: Array<{ role: string; content: string }>;
		temperature?: number;
		max_tokens?: number;
		top_p?: number;
		min_p?: number;
		top_k?: number;
		frequency_penalty?: number;
		presence_penalty?: number;
		signal?: AbortSignal;
	}): Promise<Response> => {
		return fetchOK(
			`${API_BASE}/api/chat/arena`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify(body),
				...(body.signal ? { signal: body.signal } : {}),
			},
			"Arena failed",
		);
	},
};
