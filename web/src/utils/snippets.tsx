import React, { type ReactNode } from "react";
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

export function snippetCurlModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-white font-semibold terminal-highlight">
				{"curl -X POST "}
			</span>
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1/chat/completions`}
			</span>
			{" \\\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"-H"}</span>{" "}
			<span className="text-[#ce9178]">{'"Authorization: Bearer '}</span>
			<span className="text-white font-semibold terminal-highlight">
				{"YOUR_API_KEY"}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{" \\\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"-H"}</span>{" "}
			<span className="text-[#ce9178]">
				{'"Content-Type: application/json"'}
			</span>
			{" \\\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"-d"}</span>{" "}
			<span className="text-[#ce9178]">{"'{"}</span>
			<span className="text-[#9cdcfe]">{'"model"'}</span>
			<span className="text-[#ce9178]">{':"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'","messages"'}</span>
			<span className="text-[#ce9178]">
				{':[{"role":"user","content":"Hello"}]}'}
			</span>
			<span className="text-[#ce9178]">{"'"}</span>
		</>
	);
}

export function snippetCurlModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return snippetCurl({ proxyModelId, origin });
}

export function snippetZedModel({
	proxyModelId,
	displayName,
	contextLength,
	maxOutputTokens,
	capabilities,
	origin,
}: ZedSnippetOpts): ReactNode {
	return (
		<>
			{"{"}
			{"\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"language_models"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{'"openai_compatible"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"model-hotel"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{'"api_url"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{'"available_models"'}</span>
			<span className="text-[#ce9178]">{": ["}</span>
			{"\n"}
			{"          {"}
			{"\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"name"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"display_name"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{displayName}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"max_tokens"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{String(contextLength)}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"max_output_tokens"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{String(maxOutputTokens)}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"capabilities"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"tools"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "tool_calling"))}
			</span>
			{",\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"images"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "vision"))}
			</span>
			{",\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"parallel_tool_calls"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "parallel_tool_calls"))}
			</span>
			{",\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"prompt_cache_key"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">{"false"}</span>
			{",\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"chat_completions"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">{"true"}</span>
			{",\n"}
			{"              "}
			<span className="text-[#9cdcfe]">{'"interleaved_reasoning"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "reasoning"))}
			</span>
			{"\n"}
			{"            "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"          "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"        "}
			<span className="text-[#ce9178]">{"]"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"  "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"}"}
		</>
	);
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

export function snippetOpencodeModel({
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
}: OpencodeSnippetOpts): ReactNode {
	const hasCost = inputPricePerMillion != null && outputPricePerMillion != null;
	const hasAttachment = inputModalities.some((m) => m !== "text");
	const inputMods = inputModalities.length > 0 ? inputModalities : ["text"];
	const outputMods = outputModalities.length > 0 ? outputModalities : ["text"];

	return (
		<>
			{"{"}
			{"\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"provider"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{'"model-hotel"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"npm"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-[#ce9178]">{"@ai-sdk/openai-compatible"}</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"name"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-[#ce9178]">{"Model Hotel"}</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"options"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{'"baseURL"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#ce9178]">{"}"}</span>
			{",\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"models"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"        "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-[#ce9178]">{displayName}</span>
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"id"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"attachment"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">{String(hasAttachment)}</span>
			{",\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"reasoning"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "reasoning"))}
			</span>
			{",\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"tool_call"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#569cd6]">
				{String(hasCap(capabilities, "tool_calling"))}
			</span>
			{",\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"limit"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"context"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{String(contextLength)}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"output"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{String(maxOutputTokens)}</span>
			{"\n"}
			{"          "}
			<span className="text-[#ce9178]">{"}"}</span>
			{",\n"}
			{"          "}
			<span className="text-[#9cdcfe]">{'"modalities"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"input"'}</span>
			<span className="text-[#ce9178]">{": ["}</span>
			{inputMods.map((m, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: stable modalities array with comma logic
				<React.Fragment key={`${m}-${i}`}>
					<span className="text-[#ce9178]">{'"'}</span>
					<span className="text-[#ce9178]">{m}</span>
					<span className="text-[#ce9178]">{'"'}</span>
					{i < inputMods.length - 1 && (
						<span className="text-[#ce9178]">{", "}</span>
					)}
				</React.Fragment>
			))}
			<span className="text-[#ce9178]">{"]"}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"output"'}</span>
			<span className="text-[#ce9178]">{": ["}</span>
			{outputMods.map((m, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: stable modalities array with comma logic
				<React.Fragment key={`${m}-${i}`}>
					<span className="text-[#ce9178]">{'"'}</span>
					<span className="text-[#ce9178]">{m}</span>
					<span className="text-[#ce9178]">{'"'}</span>
					{i < outputMods.length - 1 && (
						<span className="text-[#ce9178]">{", "}</span>
					)}
				</React.Fragment>
			))}
			<span className="text-[#ce9178]">{"]"}</span>
			{"\n"}
			{"          "}
			<span className="text-[#ce9178]">{"}"}</span>
			{hasCost && (
				<>
					{",\n"}
					{"          "}
					<span className="text-[#9cdcfe]">{'"cost"'}</span>
					<span className="text-[#ce9178]">{": {"}</span>
					{"\n"}
					{"            "}
					<span className="text-[#9cdcfe]">{'"input"'}</span>
					<span className="text-[#ce9178]">{": "}</span>
					<span className="text-[#b5cea8]">{String(inputPricePerMillion)}</span>
					{",\n"}
					{"            "}
					<span className="text-[#9cdcfe]">{'"output"'}</span>
					<span className="text-[#ce9178]">{": "}</span>
					<span className="text-[#b5cea8]">
						{String(outputPricePerMillion)}
					</span>
					{"\n"}
					{"          "}
					<span className="text-[#ce9178]">{"}"}</span>
				</>
			)}
			{"\n"}
			{"        "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"  "}
			<span className="text-[#ce9178]">{"}"}</span>
			{"\n"}
			{"}"}
		</>
	);
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

export function snippetJSModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"import "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			<span className="text-[#c586c0]">{" from "}</span>
			<span className="text-[#ce9178]">{'"openai"'}</span>
			{";\n\n"}
			<span className="text-[#c586c0]">{"const "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#c586c0]">{" = new "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"({\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"apiKey"}</span>
			{": "}
			<span className="text-[#4ec9b0]">{"process"}</span>
			<span className="text-[#9cdcfe]">{".env"}</span>
			<span className="text-[#9cdcfe]">{"["}</span>
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			<span className="text-[#9cdcfe]">{"]"}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"baseURL"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n});\n\n"}
			<span className="text-[#c586c0]">{"const "}</span>
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#c586c0]">{" = await "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#9cdcfe]">{".chat"}</span>
			<span className="text-[#9cdcfe]">{".completions"}</span>
			<span className="text-[#9cdcfe]">{".create"}</span>
			{"({\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"model"}</span>
			{": "}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"messages"}</span>
			{": [{"}
			<span className="text-[#9cdcfe]">{"role"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"user"'}</span>
			{", "}
			<span className="text-[#9cdcfe]">{"content"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Hello!"'}</span>
			{"}],\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"max_tokens"}</span>
			{": "}
			<span className="text-[#ce9178]">{"128"}</span>
			{"\n});\n\n"}
			<span className="text-[#4ec9b0]">{"console"}</span>
			<span className="text-[#9cdcfe]">{".log"}</span>
			{"("}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#9cdcfe]">{".choices"}</span>
			{"[0]?."}
			<span className="text-[#9cdcfe]">{"message"}</span>
			<span className="text-[#9cdcfe]">{"?.content"}</span>
			{");"}
		</>
	);
}

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

export function snippetPythonModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"import "}</span>
			<span className="text-[#4ec9b0]">{"os"}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"from "}</span>
			<span className="text-[#4ec9b0]">{"openai"}</span>
			<span className="text-[#c586c0]">{" import "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"\n\n"}
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#c586c0]">{" = "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"(\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"api_key"}</span>
			{"="}
			<span className="text-[#4ec9b0]">{"os"}</span>
			<span className="text-[#9cdcfe]">{".environ"}</span>
			{"["}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{"],\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"base_url"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n)\n\n"}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#c586c0]">{" = "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#9cdcfe]">{".chat"}</span>
			<span className="text-[#9cdcfe]">{".completions"}</span>
			<span className="text-[#9cdcfe]">{".create"}</span>
			{"(\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"model"}</span>
			{"="}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			{",\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"messages"}</span>
			{"=[{"}
			<span className="text-[#9cdcfe]">{"role"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"user"'}</span>
			{", "}
			<span className="text-[#9cdcfe]">{"content"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Hello!"'}</span>
			{"}],\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"max_tokens"}</span>
			{"="}
			<span className="text-[#ce9178]">{"128"}</span>
			{",\n)\n\n"}
			<span className="text-[#4ec9b0]">{"print"}</span>
			{"("}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#9cdcfe]">{".choices"}</span>
			{"[0]."}
			<span className="text-[#9cdcfe]">{"message"}</span>
			<span className="text-[#9cdcfe]">{".content"}</span>
			{")"}
		</>
	);
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

export function snippetClaudeCodeModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_BASE_URL"}</span>
			{"="}
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_API_KEY"}</span>
			{"="}
			<span className="text-[#ce9178]">{"YOUR_API_KEY"}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_OPUS_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_SONNET_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_HAIKU_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"CLAUDE_CODE_SUBAGENT_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
		</>
	);
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

export function snippetOpenClawModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-white font-semibold terminal-highlight">
				{"openclaw"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>
			{" models.providers.model-hotel "}
			<span className="text-[#ce9178]">{"\"$(cat <<'JSON'"}</span>
			{"\n{\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"baseUrl"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"api"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"openai-completions"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"auth"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"api-key"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"apiKey"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"models"'}</span>
			{": [{"}
			<span className="text-[#9cdcfe]">{'"id"'}</span>
			{": "}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			{", "}
			<span className="text-[#9cdcfe]">{'"name"'}</span>
			{": "}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			{"}]\n"}
			{"}\n"}
			<span className="text-[#ce9178]">{"JSON"}</span>
			{"\n"}
			<span className="text-[#ce9178]">{'")"'}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"openclaw"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"models"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>{" "}
			<span className="text-white font-semibold terminal-highlight">
				{`model-hotel/${proxyModelId}`}
			</span>
		</>
	);
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

export function snippetHermesModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>{" "}
			<span className="text-[#9cdcfe]">{"OPENAI_BASE_URL"}</span>{" "}
			<span className="text-white font-semibold terminal-highlight">{`${origin}/v1`}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>{" "}
			<span className="text-[#9cdcfe]">{"OPENAI_API_KEY"}</span>{" "}
			<span className="text-[#ce9178]">{"YOUR_API_KEY"}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>
			{" model "}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
		</>
	);
}

export function snippetHermesModelText({
	proxyModelId,
	origin,
}: ModelSnippetOpts): string {
	return `hermes config set OPENAI_BASE_URL ${origin}/v1
hermes config set OPENAI_API_KEY YOUR_API_KEY
hermes config set model ${proxyModelId}`;
}

export function snippetLibreChatModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"endpoints"}</span>
			{":\n"}
			{"  "}
			<span className="text-[#c586c0]">{"custom"}</span>
			{":\n"}
			{"    - "}
			<span className="text-[#9cdcfe]">{"name"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Model Hotel"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"baseURL"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"apiKey"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"models"}</span>
			{":\n"}
			{"        "}
			<span className="text-[#c586c0]">{"default"}</span>
			{":\n"}
			{"          - "}
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			{"\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{"fetch"}</span>
			{": "}
			<span className="text-[#569cd6]">{"false"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"titleConvo"}</span>
			{": "}
			<span className="text-[#569cd6]">{"true"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"modelDisplayLabel"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Model Hotel"'}</span>
		</>
	);
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
			<span className="text-white font-semibold terminal-highlight">
				model_name
			</span>
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
			<span className="ps-str text-[#ce9178]">{'"model_name"'}</span>
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

export function snippetPowershellModel({
	proxyModelId,
	origin,
}: ModelSnippetOpts): ReactNode {
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
			<span className="ps-str text-[#ce9178]">{'"Bearer '}</span>
			<span className="text-white font-semibold terminal-highlight">
				{"YOUR_API_KEY"}
			</span>
			<span className="ps-str text-[#ce9178]">{'"'}</span>
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
			<span className="ps-str text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{proxyModelId}
			</span>
			<span className="ps-str text-[#ce9178]">{'"'}</span>
			{"\n"}
			{"    messages = @(\n"}
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

export function snippetJS({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"import "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			<span className="text-[#c586c0]">{" from "}</span>
			<span className="text-[#ce9178]">{'"openai"'}</span>
			{";\n\n"}
			<span className="text-[#c586c0]">{"const "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#c586c0]">{" = new "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"({\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"apiKey"}</span>
			{": "}
			<span className="text-[#4ec9b0]">{"process"}</span>
			<span className="text-[#9cdcfe]">{".env"}</span>
			<span className="text-[#9cdcfe]">{"["}</span>
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			<span className="text-[#9cdcfe]">{"]"}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"baseURL"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n});\n\n"}
			<span className="text-[#c586c0]">{"const "}</span>
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#c586c0]">{" = await "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#9cdcfe]">{".chat"}</span>
			<span className="text-[#9cdcfe]">{".completions"}</span>
			<span className="text-[#9cdcfe]">{".create"}</span>
			{"({\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"model"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"messages"}</span>
			{": [{"}
			<span className="text-[#9cdcfe]">{"role"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"user"'}</span>
			{", "}
			<span className="text-[#9cdcfe]">{"content"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Hello!"'}</span>
			{"}],\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{"max_tokens"}</span>
			{": "}
			<span className="text-[#ce9178]">{"128"}</span>
			{"\n});\n\n"}
			<span className="text-[#4ec9b0]">{"console"}</span>
			<span className="text-[#9cdcfe]">{".log"}</span>
			{"("}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#9cdcfe]">{".choices"}</span>
			{"[0]?."}
			<span className="text-[#9cdcfe]">{"message"}</span>
			<span className="text-[#9cdcfe]">{"?.content"}</span>
			{");"}
		</>
	);
}

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

export function snippetPython({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"import "}</span>
			<span className="text-[#4ec9b0]">{"os"}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"from "}</span>
			<span className="text-[#4ec9b0]">{"openai"}</span>
			<span className="text-[#c586c0]">{" import "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"\n\n"}
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#c586c0]">{" = "}</span>
			<span className="text-[#4ec9b0]">{"OpenAI"}</span>
			{"(\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"api_key"}</span>
			{"="}
			<span className="text-[#4ec9b0]">{"os"}</span>
			<span className="text-[#9cdcfe]">{".environ"}</span>
			{"["}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{"],\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"base_url"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n)\n\n"}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#c586c0]">{" = "}</span>
			<span className="text-[#4ec9b0]">{"client"}</span>
			<span className="text-[#9cdcfe]">{".chat"}</span>
			<span className="text-[#9cdcfe]">{".completions"}</span>
			<span className="text-[#9cdcfe]">{".create"}</span>
			{"(\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"model"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{",\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"messages"}</span>
			{"=[{"}
			<span className="text-[#9cdcfe]">{"role"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"user"'}</span>
			{", "}
			<span className="text-[#9cdcfe]">{"content"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Hello!"'}</span>
			{"}],\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{"max_tokens"}</span>
			{"="}
			<span className="text-[#ce9178]">{"128"}</span>
			{",\n)\n\n"}
			<span className="text-[#4ec9b0]">{"print"}</span>
			{"("}
			<span className="text-[#4ec9b0]">{"response"}</span>
			<span className="text-[#9cdcfe]">{".choices"}</span>
			{"[0]."}
			<span className="text-[#9cdcfe]">{"message"}</span>
			<span className="text-[#9cdcfe]">{".content"}</span>
			{")"}
		</>
	);
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

export function snippetClaudeCode({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_BASE_URL"}</span>
			{"="}
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_API_KEY"}</span>
			{"="}
			<span className="text-[#ce9178]">{"YOUR_API_KEY"}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_OPUS_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_SONNET_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"ANTHROPIC_DEFAULT_HAIKU_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{"\n"}
			<span className="text-[#c586c0]">{"export "}</span>
			<span className="text-[#9cdcfe]">{"CLAUDE_CODE_SUBAGENT_MODEL"}</span>
			{"="}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
		</>
	);
}

export function snippetClaudeCodeText({ origin }: BashSnippetOpts): string {
	return `export ANTHROPIC_BASE_URL=${origin}/v1
export ANTHROPIC_API_KEY=YOUR_API_KEY
export ANTHROPIC_DEFAULT_OPUS_MODEL="model_name"
export ANTHROPIC_DEFAULT_SONNET_MODEL="model_name"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="model_name"
export CLAUDE_CODE_SUBAGENT_MODEL="model_name"`;
}

export function snippetOpenClaw({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-white font-semibold terminal-highlight">
				{"openclaw"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>
			{" models.providers.model-hotel "}
			<span className="text-[#ce9178]">{"\"$(cat <<'JSON'"}</span>
			{"\n{\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"baseUrl"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"api"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"openai-completions"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"auth"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"api-key"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"apiKey"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"models"'}</span>
			{": [{"}
			<span className="text-[#9cdcfe]">{'"id"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{", "}
			<span className="text-[#9cdcfe]">{'"name"'}</span>
			{": "}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{"}]\n"}
			{"}\n"}
			<span className="text-[#ce9178]">{"JSON"}</span>
			{"\n"}
			<span className="text-[#ce9178]">{'")"'}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"openclaw"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"models"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>
			{" model-hotel/model_name"}
		</>
	);
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

export function snippetHermes({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>{" "}
			<span className="text-[#9cdcfe]">{"OPENAI_BASE_URL"}</span>{" "}
			<span className="text-white font-semibold terminal-highlight">{`${origin}/v1`}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>{" "}
			<span className="text-[#9cdcfe]">{"OPENAI_API_KEY"}</span>{" "}
			<span className="text-[#ce9178]">{"YOUR_API_KEY"}</span>
			{"\n"}
			<span className="text-white font-semibold terminal-highlight">
				{"hermes"}
			</span>{" "}
			<span className="text-[#4ec9b0]">{"config"}</span>{" "}
			<span className="text-[#4ec9b0]">{"set"}</span>
			{" model "}
			<span className="text-[#ce9178]">{"model_name"}</span>
		</>
	);
}

export function snippetHermesText({ origin }: BashSnippetOpts): string {
	return `hermes config set OPENAI_BASE_URL ${origin}/v1
hermes config set OPENAI_API_KEY YOUR_API_KEY
hermes config set model model_name`;
}

export function snippetLibreChat({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			<span className="text-[#c586c0]">{"endpoints"}</span>
			{":\n"}
			{"  "}
			<span className="text-[#c586c0]">{"custom"}</span>
			{":\n"}
			{"    - "}
			<span className="text-[#9cdcfe]">{"name"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Model Hotel"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"baseURL"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{origin}
			</span>
			<span className="text-[#ce9178]">{'/v1"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"apiKey"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"YOUR_API_KEY"'}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"models"}</span>
			{":\n"}
			{"        "}
			<span className="text-[#c586c0]">{"default"}</span>
			{":\n"}
			{"          - "}
			<span className="text-[#ce9178]">{'"model_name"'}</span>
			{"\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{"fetch"}</span>
			{": "}
			<span className="text-[#569cd6]">{"false"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"titleConvo"}</span>
			{": "}
			<span className="text-[#569cd6]">{"true"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{"modelDisplayLabel"}</span>
			{": "}
			<span className="text-[#ce9178]">{'"Model Hotel"'}</span>
		</>
	);
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

export function snippetZedVK({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			{"{"}
			{"\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"language_models"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{'"openai_compatible"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"model-hotel"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{'"api_url"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"        "}
			<span className="text-[#9cdcfe]">{'"available_models"'}</span>
			<span className="text-[#ce9178]">{": ["}</span>
			{"\n"}
			{"          {"}
			{"\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"name"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{"model_name"}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"max_tokens"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{"128000"}</span>
			{",\n"}
			{"            "}
			<span className="text-[#9cdcfe]">{'"max_output_tokens"'}</span>
			<span className="text-[#ce9178]">{": "}</span>
			<span className="text-[#b5cea8]">{"16384"}</span>
			{"\n"}
			{"          }"}
			{"\n"}
			{"        ]"}
			{"\n"}
			{"      }"}
			{"\n"}
			{"    }"}
			{"\n"}
			{"  }"}
			{"\n"}
			{"}"}
		</>
	);
}

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

export function snippetOpencodeVK({ origin }: BashSnippetOpts): ReactNode {
	return (
		<>
			{"{"}
			{"\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"providers"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{'"model-hotel"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"url"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{`${origin}/v1`}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{",\n"}
			{"      "}
			<span className="text-[#9cdcfe]">{'"apiKey"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{"YOUR_API_KEY"}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			{"    }"}
			{"\n"}
			{"  }"}
			{",\n"}
			{"  "}
			<span className="text-[#9cdcfe]">{'"models"'}</span>
			<span className="text-[#ce9178]">{": {"}</span>
			{"\n"}
			{"    "}
			<span className="text-[#9cdcfe]">{'"default"'}</span>
			<span className="text-[#ce9178]">{': "'}</span>
			<span className="text-white font-semibold terminal-highlight">
				{"model_name"}
			</span>
			<span className="text-[#ce9178]">{'"'}</span>
			{"\n"}
			{"  }"}
			{"\n"}
			{"}"}
		</>
	);
}

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
