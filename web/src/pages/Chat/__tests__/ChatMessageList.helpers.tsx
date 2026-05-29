import { vi } from "vitest";
import type { ChatMessage, Model } from "../../api/types";

export const mockMessages: ChatMessage[] = [
	{
		role: "user",
		content: "Hello, how are you?",
		timestamp: Date.now(),
	},
	{
		role: "assistant",
		content: "I'm doing well, thank you!",
		model: "Ollama Cloud/gemma3:4b",
		timestamp: Date.now() + 1000,
		metrics: {
			durationMs: 500,
			promptTokens: 10,
			completionTokens: 20,
			tokensPerSecond: 40,
		},
	},
	{
		role: "assistant",
		content: "This is model B's response.",
		model: "Ollama Cloud/glm-5",
		timestamp: Date.now() + 2000,
		metrics: {
			durationMs: 600,
			promptTokens: 12,
			completionTokens: 25,
			tokensPerSecond: 41.67,
		},
	},
];

const baseModel = {
	description: "",
	display_name: "",
	provider_id: "provider-1",
	provider_name: "Ollama Cloud",
	capabilities: '{"vision":false,"audio_input":false,"reasoning":false}',
	params: "{}",
	modality: "text",
	input_modalities: "text",
	output_modalities: "text",
	input_price_per_million: null,
	input_price_per_million_cache_hit: null,
	output_price_per_million: null,
	enabled: true,
	disabled_manually: false,
	created_at: "2024-01-01T00:00:00Z",
	last_seen_at: "2024-01-01T00:00:00Z",
} satisfies Omit<
	Model,
	| "id"
	| "model_id"
	| "name"
	| "context_length"
	| "max_output_tokens"
	| "owned_by"
>;

export const mockEnabledModels: Model[] = [
	{
		...baseModel,
		id: "model-1",
		model_id: "gemma3:4b",
		name: "gemma3:4b",
		display_name: "gemma3:4b",
		context_length: 8192,
		max_output_tokens: 4096,
		owned_by: "ollama",
	},
	{
		...baseModel,
		id: "model-2",
		model_id: "glm-5",
		name: "glm-5",
		display_name: "glm-5",
		context_length: 32768,
		max_output_tokens: 8192,
		owned_by: "zhipu",
	},
];

const defaultProps = {
	messages: mockMessages,
	chatSubMode: "chat" as const,
	isStreaming: false,
	selectedModelB: "Ollama Cloud/glm-5",
	enabledModels: mockEnabledModels,
	onStopConversation: vi.fn(),
	onStop: vi.fn(),
	onRegenerate: vi.fn(),
	onDeleteMessage: vi.fn(),
	activePersonaIdB: null as string | null,
	conversationActivePersonaIdA: null as string | null,
	chatActivePersonaId: null as string | null,
};

export function createDefaultProps(overrides?: Partial<typeof defaultProps>) {
	return { ...defaultProps, ...overrides };
}
