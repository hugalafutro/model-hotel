import type { ReactNode } from "react";
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

export function snippetCurl({ proxyModelId, origin }: CurlSnippetOpts): string {
	return `curl -X POST ${origin}/v1/chat/completions \\\n  -H "Authorization: Bearer API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${proxyModelId}","messages":[{"role":"user","content":"Hello"}]}'`;
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
// Virtual-keys snippets (JSX with syntax highlighting)
// ---------------------------------------------------------------------------

export interface BashSnippetOpts {
	origin: string;
}

export function snippetBash({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			{"curl -X POST "}
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}`}
				{"/v1/chat/completions"}
			</span>
			{" \\\n"}
			{'  -H "Authorization: Bearer '}
			<span className="text-white font-semibold terminal-highlight">
				YOUR_API_KEY
			</span>
			{'" \\\n'}
			{'  -H "Content-Type: application/json" \\\n'}
			{"  -d '{\n"}
			{'    "model": "'}
			<span className="text-white font-semibold terminal-highlight">model</span>
			{'",\n'}
			{'    "messages": [\n'}
			{'      { "role": "user", "content": "Hello!" }\n'}
			{"    ]\n"}
			{"  }'"}
		</>
	);
}

export interface PowershellSnippetOpts {
	origin: string;
}

export function snippetPowershell({
	origin,
}: PowershellSnippetOpts): ReactNode {
	return (
		<>
			{"Invoke-RestMethod "}
			{"-Uri "}
			<span className="ps-uri text-[#569cd6]">
				{`"${origin}/v1/chat/completions"`}
			</span>
			{"\n"}
			{"  -Method Post\n"}
			{"  -Headers @{\n"}
			{"    "}
			<span className="ps-key text-[#9cdcfe]">{'"Authorization"'}</span>
			{" = "}
			<span className="ps-str text-[#ce9178]">{'"Bearer YOUR_API_KEY"'}</span>
			{"\n"}
			{"    "}
			<span className="ps-key text-[#9cdcfe]">{'"Content-Type"'}</span>
			{" = "}
			<span className="ps-str text-[#ce9178]">{'"application/json"'}</span>
			{"\n"}
			{"  }\n"}
			{"  -Body (ConvertTo-Json @{\n"}
			{"    "}
			<span className="ps-key text-[#9cdcfe]">{"model"}</span>
			{" = "}
			<span className="ps-str text-[#ce9178]">{'"model"'}</span>
			{"\n"}
			{"    "}
			<span className="ps-key text-[#9cdcfe]">{"messages"}</span>
			{" = @(\n"}
			{"      @{ "}
			<span className="ps-key text-[#9cdcfe]">{"role"}</span>
			{" = "}
			<span className="ps-str text-[#ce9178]">{'"user"'}</span>
			{"; "}
			<span className="ps-key text-[#9cdcfe]">{"content"}</span>
			{" = "}
			<span className="ps-str text-[#ce9178]">{'"Hello!"'}</span>
			{" }\n"}
			{"    )\n"}
			{"  })"}
		</>
	);
}
