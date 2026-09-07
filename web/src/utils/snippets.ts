import { hasCap } from "../components/capMeta";

/**
 * The stand-in the snippets carry where a real key goes. The virtual-key panel
 * swaps it for the key it just revealed, so the sentinel is spelled once and
 * every template interpolates it.
 */
export const KEY_PLACEHOLDER = "YOUR_API_KEY";

// The virtual-key snippets are the model snippets with the model name left as a
// placeholder: the panel shows how to call the proxy, and which model to name is
// the operator's choice.
const VK_MODEL = "model_name";

/**
 * The address the browser is reading the dashboard from, which is also where
 * the proxy answers. A snippet is copied into a shell elsewhere, so it needs
 * the absolute URL rather than a relative path.
 */
export function proxyOrigin(): string {
	return window.location.origin;
}

// ---------------------------------------------------------------------------
// Model-detail snippets (plain text strings)
// ---------------------------------------------------------------------------

export interface ModelSnippetOpts {
	proxyModelId: string;
	origin: string;
}

export function snippetCurlModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `curl -X POST ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer ${KEY_PLACEHOLDER}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${proxyModelId}","messages":[{"role":"user","content":"Hello"}]}'`;
}

export interface ZedSnippetOpts {
	proxyModelId: string;
	displayName: string;
	contextLength: number | null;
	maxOutputTokens: number | null;
	capabilities: Record<string, boolean> | null;
	origin: string;
}

export function snippetZedModelText({
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

// ---------------------------------------------------------------------------
// SDK & tool snippets (model-detail variants)
// ---------------------------------------------------------------------------

export function snippetJSModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.${KEY_PLACEHOLDER},
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
    api_key=os.environ["${KEY_PLACEHOLDER}"],
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
export ANTHROPIC_API_KEY=${KEY_PLACEHOLDER}
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
  "apiKey": "${KEY_PLACEHOLDER}",
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
hermes config set OPENAI_API_KEY ${KEY_PLACEHOLDER}
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
      apiKey: "${KEY_PLACEHOLDER}"
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

export function snippetBashText({ origin }: BashSnippetOpts): string {
	return `curl -X POST ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer ${KEY_PLACEHOLDER}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${VK_MODEL}",
    "messages": [
      { "role": "user", "content": "Hello!" }
    ]
  }'`;
}

export function snippetPowershellModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `Invoke-RestMethod -Uri "${origin}/v1/chat/completions"
  -Method Post
  -Headers @{
    "Authorization" = "Bearer ${KEY_PLACEHOLDER}"
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

export const snippetPowershellText = ({ origin }: BashSnippetOpts): string =>
	snippetPowershellModelText({ proxyModelId: VK_MODEL, origin });

export const snippetJSText = ({ origin }: BashSnippetOpts): string =>
	snippetJSModelText({ proxyModelId: VK_MODEL, origin });

export const snippetPythonText = ({ origin }: BashSnippetOpts): string =>
	snippetPythonModelText({ proxyModelId: VK_MODEL, origin });

export const snippetClaudeCodeText = ({ origin }: BashSnippetOpts): string =>
	snippetClaudeCodeModelText({ proxyModelId: VK_MODEL, origin });

export const snippetOpenClawText = ({ origin }: BashSnippetOpts): string =>
	snippetOpenClawModelText({ proxyModelId: VK_MODEL, origin });

export const snippetHermesText = ({ origin }: BashSnippetOpts): string =>
	snippetHermesModelText({ proxyModelId: VK_MODEL, origin });

export const snippetLibreChatText = ({ origin }: BashSnippetOpts): string =>
	snippetLibreChatModelText({ proxyModelId: VK_MODEL, origin });

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
								name: VK_MODEL,
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
					apiKey: KEY_PLACEHOLDER,
				},
			},
			models: {
				default: VK_MODEL,
			},
		},
		null,
		2,
	);
}
