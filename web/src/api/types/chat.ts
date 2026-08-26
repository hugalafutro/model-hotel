export interface GenerationParams {
	temperature?: number;
	max_tokens?: number;
	top_p?: number;
	min_p?: number;
	top_k?: number;
	frequency_penalty?: number;
	presence_penalty?: number;
	reasoning_effort?: string; // "low" | "medium" | "high" — OpenAI o1/o3 reasoning depth
}
/** OpenAI-compatible multimodal content part types */
export type TextContentPart = { type: "text"; text: string };
export type ImageContentPart = {
	type: "image_url";
	image_url: { url: string };
};
export type AudioContentPart = {
	type: "input_audio";
	input_audio: { data: string; format: string };
};
export type ContentPart = TextContentPart | ImageContentPart | AudioContentPart;
export type MessageContent = string | ContentPart[];
export interface ChatMessage {
	role: "user" | "assistant" | "system";
	content: string;
	imageUrl?: string;
	audioAttachment?: { data: string; format: string };
	rawContent?: string;
	thinkingContent?: string;
	error?: string | null;
	aborted?: boolean;
	model?: string;
	timestamp: number;
	metrics?: {
		tokensPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	};
	params?: GenerationParams;
}
