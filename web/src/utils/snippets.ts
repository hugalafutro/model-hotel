import { hasCap } from "../components/capMeta";

export type SnippetTab = "curl" | "zed" | "opencode" | "bash" | "powershell";

/** Tab display labels */
export const SNIPPET_TAB_LABELS: Record<SnippetTab, string> = {
	curl: "cURL",
	zed: "ZED",
	opencode: "OpenCode",
	bash: "Bash",
	powershell: "PowerShell",
};

// ---------------------------------------------------------------------------
// Model-detail snippets (plain text strings)
// ---------------------------------------------------------------------------

export interface CurlSnippetOpts {
	proxyModelId: string;
	origin: string;
}

export interface ModelSnippetOpts {
	proxyModelId: string;
	origin: string;
}

export function snippetCurl({ proxyModelId, origin }: CurlSnippetOpts): string {
	return `curl -X POST ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer YOUR_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${proxyModelId}","messages":[{"role":"user","content":"Hello"}]}'`;
}

export interface ZedSnippetOpts {
	proxyModelId: string;
	displayName: string;
	contextLength: number | null;
	maxOutputTokens: number | null;
	capabilities: Record<string, boolean> | null;
	origin: string;
}

export function snippetZed({
	proxyModelId,
	displayName,
	contextLength,
	maxOutputTokens,
	capabilities,
	origin,
}: ZedSnippetOpts): string {
	return JSON.stringify(
		{
			language_models: {
				openai_compatible: {
					"model-hotel": {
						api_url: `${origin}/v1`,
						available_models: [
							{
								name: proxyModelId,
								display_name: displayName,
								max_tokens: contextLength,
								max_output_tokens: maxOutputTokens,
								capabilities: {
									tools: hasCap(capabilities, "tool_calling"),
									images: hasCap(capabilities, "vision"),
									parallel_tool_calls: hasCap(
										capabilities,
										"parallel_tool_calls",
									),
									prompt_cache_key: false,
									chat_completions: true,
									interleaved_reasoning: hasCap(capabilities, "reasoning"),
								},
							},
						],
					},
				},
			},
		},
		null,
		2,
	);
}

export interface OpencodeSnippetOpts {
	proxyModelId: string;
	displayName: string;
	contextLength: number | null;
	maxOutputTokens: number | null;
	capabilities: Record<string, boolean> | null;
	inputModalities: string[];
	outputModalities: string[];
	inputPricePerMillion: number | null;
	outputPricePerMillion: number | null;
	origin: string;
}

export function snippetOpencode({
	proxyModelId,
	displayName,
	contextLength,
	maxOutputTokens,
	capabilities,
	inputModalities,
	outputModalities,
	inputPricePerMillion,
	outputPricePerMillion,
	origin,
}: OpencodeSnippetOpts): string {
	return JSON.stringify(
		{
			provider: {
				"model-hotel": {
					npm: "@ai-sdk/openai-compatible",
					name: "Model Hotel",
					options: {
						baseURL: `${origin}/v1`,
					},
					models: {
						[displayName]: {
							id: proxyModelId,
							attachment: inputModalities.some((m) => m !== "text"),
							reasoning: hasCap(capabilities, "reasoning"),
							tool_call: hasCap(capabilities, "tool_calling"),
							limit: {
								context: contextLength,
								output: maxOutputTokens,
							},
							modalities: {
								input: inputModalities.length > 0 ? inputModalities : ["text"],
								output:
									outputModalities.length > 0 ? outputModalities : ["text"],
							},
							...(inputPricePerMillion != null && outputPricePerMillion != null
								? {
										cost: {
											input: inputPricePerMillion,
											output: outputPricePerMillion,
										},
									}
								: {}),
						},
					},
				},
			},
		},
		null,
		2,
	);
}

// ---------------------------------------------------------------------------
// Model-detail snippets (JSX with syntax highlighting)
// ---------------------------------------------------------------------------

export function snippetCurlModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return snippetCurl({ proxyModelId, origin });
}

export function snippetZedModelText({
	proxyModelId,
	displayName,
	contextLength,
	maxOutputTokens,
	capabilities,
	origin,
}: ZedSnippetOpts): string {
	return snippetZed({
		proxyModelId,
		displayName,
		contextLength,
		maxOutputTokens,
		capabilities,
		origin,
	});
}

export function snippetOpencodeModelText({
	proxyModelId,
	displayName,
	contextLength,
	maxOutputTokens,
	capabilities,
	inputModalities,
	outputModalities,
	inputPricePerMillion,
	outputPricePerMillion,
	origin,
}: OpencodeSnippetOpts): string {
	return snippetOpencode({
		proxyModelId,
		displayName,
		contextLength,
		maxOutputTokens,
		capabilities,
		inputModalities,
		outputModalities,
		inputPricePerMillion,
		outputPricePerMillion,
		origin,
	});
}

// ---------------------------------------------------------------------------
// SDK & tool snippets (model-detail variants)
// ---------------------------------------------------------------------------

export function snippetJSModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.YOUR_API_KEY,
  baseURL: "${origin}/v1"
});

const response = await client.chat.completions.create({
  model: "${proxyModelId}",
  messages: [{ role: "user", content: "Hello!" }],
  max_tokens: 128
});

console.log(response.choices[0]?.message?.content);`;
}

export function snippetPythonModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `import os
from openai import OpenAI

client = OpenAI(
    api_key=os.environ["YOUR_API_KEY"],
    base_url="${origin}/v1"
)

response = client.chat.completions.create(
    model="${proxyModelId}",
    messages=[{"role": "user", "content": "Hello!"}],
    max_tokens=128,
)

print(response.choices[0].message.content)`;
}

export function snippetClaudeCodeModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `export ANTHROPIC_BASE_URL=${origin}/v1
export ANTHROPIC_API_KEY=YOUR_API_KEY
export ANTHROPIC_DEFAULT_OPUS_MODEL="${proxyModelId}"
export ANTHROPIC_DEFAULT_SONNET_MODEL="${proxyModelId}"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="${proxyModelId}"
export CLAUDE_CODE_SUBAGENT_MODEL="${proxyModelId}"`;
}

export function snippetOpenClawModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `openclaw config set models.providers.model-hotel "$(cat <<'JSON'
{
  "baseUrl": "${origin}/v1",
  "api": "openai-completions",
  "auth": "api-key",
  "apiKey": "YOUR_API_KEY",
  "models": [{ "id": "${proxyModelId}", "name": "${proxyModelId}" }]
}
JSON
)"
openclaw models set model-hotel/${proxyModelId}`;
}

export function snippetHermesModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `hermes config set OPENAI_BASE_URL ${origin}/v1
hermes config set OPENAI_API_KEY YOUR_API_KEY
hermes config set model ${proxyModelId}`;
}

export function snippetLibreChatModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `endpoints:
  custom:
    - name: "Model Hotel"
      baseURL: "${origin}/v1"
      apiKey: "YOUR_API_KEY"
      models:
        default:
          - "${proxyModelId}"
        fetch: false
      titleConvo: true
      modelDisplayLabel: "Model Hotel"`;
}

// ---------------------------------------------------------------------------
// Virtual-keys snippets (JSX with syntax highlighting)
// ---------------------------------------------------------------------------

export interface BashSnippetOpts {
	origin: string;
}

export interface PowershellSnippetOpts {
	origin: string;
}

export function snippetBashText({ origin }: BashSnippetOpts): string {
	return `curl -X POST ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "model_name",
    "messages": [
      { "role": "user", "content": "Hello!" }
    ]
  }'`;
}

export function snippetPowershellText({
	origin,
}: PowershellSnippetOpts): string {
	return `Invoke-RestMethod -Uri "${origin}/v1/chat/completions"
  -Method Post
  -Headers @{
    "Authorization" = "Bearer YOUR_API_KEY"
    "Content-Type" = "application/json"
  }
  -Body (ConvertTo-Json @{
    model = "model_name"
    messages = @(
      @{ role = "user"; content = "Hello!" }
    )
  })`;
}

export function snippetPowershellModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `Invoke-RestMethod -Uri "${origin}/v1/chat/completions"
  -Method Post
  -Headers @{
    "Authorization" = "Bearer YOUR_API_KEY"
    "Content-Type" = "application/json"
  }
  -Body (ConvertTo-Json @{
    model = "${proxyModelId}"
    messages = @(
      @{ role = "user"; content = "Hello!" }
    )
  })`;
}

// ---------------------------------------------------------------------------
// SDK & tool snippets
// ---------------------------------------------------------------------------

export function snippetJSText({ origin }: BashSnippetOpts): string {
	return `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.YOUR_API_KEY,
  baseURL: "${origin}/v1"
});

const response = await client.chat.completions.create({
  model: "model_name",
  messages: [{ role: "user", content: "Hello!" }],
  max_tokens: 128
});

console.log(response.choices[0]?.message?.content);`;
}

export function snippetPythonText({ origin }: BashSnippetOpts): string {
	return `import os
from openai import OpenAI

client = OpenAI(
    api_key=os.environ["YOUR_API_KEY"],
    base_url="${origin}/v1"
)

response = client.chat.completions.create(
    model="model_name",
    messages=[{"role": "user", "content": "Hello!"}],
    max_tokens=128,
)

print(response.choices[0].message.content)`;
}

export function snippetClaudeCodeText({ origin }: BashSnippetOpts): string {
	return `export ANTHROPIC_BASE_URL=${origin}/v1
export ANTHROPIC_API_KEY=YOUR_API_KEY
export ANTHROPIC_DEFAULT_OPUS_MODEL="model_name"
export ANTHROPIC_DEFAULT_SONNET_MODEL="model_name"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="model_name"
export CLAUDE_CODE_SUBAGENT_MODEL="model_name"`;
}

export function snippetOpenClawText({ origin }: BashSnippetOpts): string {
	return `openclaw config set models.providers.model-hotel "$(cat <<'JSON'
{
  "baseUrl": "${origin}/v1",
  "api": "openai-completions",
  "auth": "api-key",
  "apiKey": "YOUR_API_KEY",
  "models": [{ "id": "model_name", "name": "model_name" }]
}
JSON
)"
openclaw models set model-hotel/model_name`;
}

export function snippetHermesText({ origin }: BashSnippetOpts): string {
	return `hermes config set OPENAI_BASE_URL ${origin}/v1
hermes config set OPENAI_API_KEY YOUR_API_KEY
hermes config set model model_name`;
}

export function snippetLibreChatText({ origin }: BashSnippetOpts): string {
	return `endpoints:
  custom:
    - name: "Model Hotel"
      baseURL: "${origin}/v1"
      apiKey: "YOUR_API_KEY"
      models:
        default:
          - "model_name"
        fetch: false
      titleConvo: true
      modelDisplayLabel: "Model Hotel"`;
}

// ---------------------------------------------------------------------------
// Virtual-key ZED snippet
// ---------------------------------------------------------------------------

export function snippetZedVKText({ origin }: BashSnippetOpts): string {
	return JSON.stringify(
		{
			language_models: {
				openai_compatible: {
					"model-hotel": {
						api_url: `${origin}/v1`,
						available_models: [
							{
								name: "model_name",
								max_tokens: 128000,
								max_output_tokens: 16384,
							},
						],
					},
				},
			},
		},
		null,
		2,
	);
}

// ---------------------------------------------------------------------------
// Virtual-key OpenCode snippet
// ---------------------------------------------------------------------------

export function snippetOpencodeVKText({ origin }: BashSnippetOpts): string {
	return JSON.stringify(
		{
			providers: {
				"model-hotel": {
					url: `${origin}/v1`,
					apiKey: "YOUR_API_KEY",
				},
			},
			models: {
				default: "model_name",
			},
		},
		null,
		2,
	);
}
