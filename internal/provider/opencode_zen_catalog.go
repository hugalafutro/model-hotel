package provider

var opencodeZenCatalog = []OpenCodeModelSpec{
	// === Free Models ===
	{
		ModelID: "big-pickle", DisplayName: "Big Pickle",
		Description:   "Stealth free model (limited time). Data may be collected to improve the model.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "minimax-m2.5-free", DisplayName: "MiniMax M2.5 Free",
		Description:   "Free tier (limited time). Data may be collected to improve the model.",
		ContextLength: 1048576, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "ling-2.6-flash-free", DisplayName: "Ling 2.6 Flash Free",
		Description:   "Free tier (limited time). Data may be collected to improve the model.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "hy3-preview-free", DisplayName: "Hy3 Preview Free",
		Description:   "Free tier (limited time). Data may be collected to improve the model.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "trinity-large-preview-free", DisplayName: "Trinity Large Preview Free",
		Description:   "Free tier (limited time).",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "nemotron-3-super-free", DisplayName: "Nemotron 3 Super Free",
		Description:   "Free tier (NVIDIA trial, limited time). Prompts/outputs logged by NVIDIA.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
	{
		ModelID: "gpt-5-nano", DisplayName: "GPT 5 Nano",
		Description:   "Free tier.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},

	// === GPT 5.5 Models ===
	{
		ModelID: "gpt-5.5", DisplayName: "GPT 5.5",
		Description:   "GPT 5.5 via OpenAI responses endpoint. Price increases to $10/$45 above 272K tokens.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 30.00,
	},
	{
		ModelID: "gpt-5.5-pro", DisplayName: "GPT 5.5 Pro",
		Description:   "GPT 5.5 Pro via OpenAI responses endpoint.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 30.00, InputPricePerMillionCacheHit: 30.00, OutputPricePerMillion: 180.00,
	},

	// === GPT 5.4 Models ===
	{
		ModelID: "gpt-5.4", DisplayName: "GPT 5.4",
		Description:   "GPT 5.4 via OpenAI responses endpoint. Price increases to $5/$22.50 above 272K tokens.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 2.50, InputPricePerMillionCacheHit: 0.25, OutputPricePerMillion: 15.00,
	},
	{
		ModelID: "gpt-5.4-pro", DisplayName: "GPT 5.4 Pro",
		Description:   "GPT 5.4 Pro via OpenAI responses endpoint.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 30.00, InputPricePerMillionCacheHit: 30.00, OutputPricePerMillion: 180.00,
	},
	{
		ModelID: "gpt-5.4-mini", DisplayName: "GPT 5.4 Mini",
		Description:   "GPT 5.4 Mini via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.75, InputPricePerMillionCacheHit: 0.075, OutputPricePerMillion: 4.50,
	},
	{
		ModelID: "gpt-5.4-nano", DisplayName: "GPT 5.4 Nano",
		Description:   "GPT 5.4 Nano via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.20, InputPricePerMillionCacheHit: 0.02, OutputPricePerMillion: 1.25,
	},

	// === GPT 5.3 Models ===
	{
		ModelID: "gpt-5.3-codex-spark", DisplayName: "GPT 5.3 Codex Spark",
		Description:   "GPT 5.3 Codex Spark via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},
	{
		ModelID: "gpt-5.3-codex", DisplayName: "GPT 5.3 Codex",
		Description:   "GPT 5.3 Codex via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},

	// === GPT 5.2 Models ===
	{
		ModelID: "gpt-5.2", DisplayName: "GPT 5.2",
		Description:   "GPT 5.2 via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},
	{
		ModelID: "gpt-5.2-codex", DisplayName: "GPT 5.2 Codex",
		Description:   "GPT 5.2 Codex via OpenAI responses endpoint. Deprecated July 23, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},

	// === GPT 5.1 Models ===
	{
		ModelID: "gpt-5.1", DisplayName: "GPT 5.1",
		Description:   "GPT 5.1 via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},
	{
		ModelID: "gpt-5.1-codex-max", DisplayName: "GPT 5.1 Codex Max",
		Description:   "GPT 5.1 Codex Max via OpenAI responses endpoint. Deprecated July 23, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.25, InputPricePerMillionCacheHit: 0.125, OutputPricePerMillion: 10.00,
	},
	{
		ModelID: "gpt-5.1-codex", DisplayName: "GPT 5.1 Codex",
		Description:   "GPT 5.1 Codex via OpenAI responses endpoint. Deprecated July 23, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},
	{
		ModelID: "gpt-5.1-codex-mini", DisplayName: "GPT 5.1 Codex Mini",
		Description:   "GPT 5.1 Codex Mini via OpenAI responses endpoint. Deprecated July 23, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.25, InputPricePerMillionCacheHit: 0.025, OutputPricePerMillion: 2.00,
	},

	// === GPT 5 Models ===
	{
		ModelID: "gpt-5", DisplayName: "GPT 5",
		Description:   "GPT 5 via OpenAI responses endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},
	{
		ModelID: "gpt-5-codex", DisplayName: "GPT 5 Codex",
		Description:   "GPT 5 Codex via OpenAI responses endpoint. Deprecated July 23, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},

	// === Claude Opus Models (Anthropic /messages endpoint) ===
	{
		ModelID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7",
		Description:   "Claude Opus 4.7 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00,
	},
	{
		ModelID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6",
		Description:   "Claude Opus 4.6 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00,
	},
	{
		ModelID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5",
		Description:   "Claude Opus 4.5 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00,
	},
	{
		ModelID: "claude-opus-4-1", DisplayName: "Claude Opus 4.1",
		Description:   "Claude Opus 4.1 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 15.00, InputPricePerMillionCacheHit: 1.50, OutputPricePerMillion: 75.00,
	},

	// === Claude Sonnet Models (Anthropic /messages endpoint) ===
	{
		ModelID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6",
		Description:   "Claude Sonnet 4.6 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00,
	},
	{
		ModelID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5",
		Description:   "Claude Sonnet 4.5 via Anthropic messages endpoint. Price increases to $6/$22.50 above 200K tokens.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00,
	},
	{
		ModelID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4",
		Description:   "Claude Sonnet 4 via Anthropic messages endpoint. Price increases to $6/$22.50 above 200K tokens. Deprecated June 15, 2026.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00,
	},

	// === Claude Haiku ===
	{
		ModelID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5",
		Description:   "Claude Haiku 4.5 via Anthropic messages endpoint.",
		ContextLength: 200000, MaxOutputTokens: 8192,
		Modality: "text", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 1.00, InputPricePerMillionCacheHit: 0.10, OutputPricePerMillion: 5.00,
	},

	// === Gemini Models ===
	{
		ModelID: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro",
		Description:   "Gemini 3.1 Pro via Google endpoint. Price increases to $4/$18 above 200K tokens.",
		ContextLength: 1000000, MaxOutputTokens: 32768,
		Modality: "vision", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 2.00, InputPricePerMillionCacheHit: 0.20, OutputPricePerMillion: 12.00,
	},
	{
		ModelID: "gemini-3-flash", DisplayName: "Gemini 3 Flash",
		Description:   "Gemini 3 Flash via Google endpoint.",
		ContextLength: 1000000, MaxOutputTokens: 16384,
		Modality: "vision", InputModalities: `["text","image"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true, Vision: true,
		InputPricePerMillion: 0.50, InputPricePerMillionCacheHit: 0.05, OutputPricePerMillion: 3.00,
	},

	// === Qwen Models ===
	{
		ModelID: "qwen3.6-plus", DisplayName: "Qwen 3.6 Plus",
		Description:   "Qwen 3.6 Plus via OpenAI-compatible chat completions.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.50, InputPricePerMillionCacheHit: 0.05, OutputPricePerMillion: 3.00,
	},
	{
		ModelID: "qwen3.5-plus", DisplayName: "Qwen 3.5 Plus",
		Description:   "Qwen 3.5 Plus via OpenAI-compatible chat completions.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.20, InputPricePerMillionCacheHit: 0.02, OutputPricePerMillion: 1.20,
	},

	// === MiniMax Models ===
	{
		ModelID: "minimax-m2.7", DisplayName: "MiniMax M2.7",
		Description:   "MiniMax M2.7 via OpenAI-compatible chat completions.",
		ContextLength: 1048576, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.30, InputPricePerMillionCacheHit: 0.06, OutputPricePerMillion: 1.20,
	},
	{
		ModelID: "minimax-m2.5", DisplayName: "MiniMax M2.5",
		Description:   "MiniMax M2.5 via OpenAI-compatible chat completions.",
		ContextLength: 1048576, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.30, InputPricePerMillionCacheHit: 0.06, OutputPricePerMillion: 1.20,
	},

	// === GLM Models ===
	{
		ModelID: "glm-5.1", DisplayName: "GLM 5.1",
		Description:   "GLM 5.1 via OpenAI-compatible chat completions.",
		ContextLength: 200000, MaxOutputTokens: 131072,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.40, InputPricePerMillionCacheHit: 0.26, OutputPricePerMillion: 4.40,
	},
	{
		ModelID: "glm-5", DisplayName: "GLM 5",
		Description:   "GLM 5 via OpenAI-compatible chat completions.",
		ContextLength: 200000, MaxOutputTokens: 131072,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.00, InputPricePerMillionCacheHit: 0.20, OutputPricePerMillion: 3.20,
	},

	// === Kimi Models ===
	{
		ModelID: "kimi-k2.6", DisplayName: "Kimi K2.6",
		Description:   "Kimi K2.6 via OpenAI-compatible chat completions.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.95, InputPricePerMillionCacheHit: 0.16, OutputPricePerMillion: 4.00,
	},
	{
		ModelID: "kimi-k2.5", DisplayName: "Kimi K2.5",
		Description:   "Kimi K2.5 via OpenAI-compatible chat completions.",
		ContextLength: 131072, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.60, InputPricePerMillionCacheHit: 0.10, OutputPricePerMillion: 3.00,
	},
}

func GetOpenCodeZenCatalog() []OpenCodeModelSpec {
	return opencodeZenCatalog
}
