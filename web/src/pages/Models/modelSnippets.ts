import type { TFunction } from "i18next";
import type { Model } from "../../api/types";
import type { LangIconKey } from "../../components/langIcons";
import type { parseCapabilities } from "../../utils/model";
import type { SnippetLang } from "../../utils/shikiHighlighter";
import {
	snippetClaudeCodeModelText,
	snippetCurlModelText,
	snippetHermesModelText,
	snippetJSModelText,
	snippetLibreChatModelText,
	snippetOpenClawModelText,
	snippetOpencodeModelText,
	snippetPowershellModelText,
	snippetPythonModelText,
	snippetZedModelText,
} from "../../utils/snippets";

export interface SnippetEntry {
	key: LangIconKey;
	title: string;
	lang: SnippetLang;
	copyText: string;
}

/** The per-client usage snippets for one model, in tab order. */
export function modelSnippetEntries({
	t,
	model,
	proxyModelId,
	caps,
	inputMods,
	outputMods,
	origin,
}: {
	t: TFunction;
	model: Model;
	proxyModelId: string;
	caps: ReturnType<typeof parseCapabilities>;
	inputMods: string[];
	outputMods: string[];
	origin: string;
}): SnippetEntry[] {
	const modelSnippetOpts = { proxyModelId, origin };
	const zedOpts = {
		proxyModelId,
		displayName: model.display_name || model.name,
		contextLength: model.context_length,
		maxOutputTokens: model.max_output_tokens,
		capabilities: caps,
		origin,
	};
	const opencodeOpts = {
		proxyModelId,
		displayName: model.display_name || model.name || proxyModelId,
		contextLength: model.context_length,
		maxOutputTokens: model.max_output_tokens,
		capabilities: caps,
		inputModalities: inputMods,
		outputModalities: outputMods,
		inputPricePerMillion: model.input_price_per_million,
		outputPricePerMillion: model.output_price_per_million,
		origin,
	};
	return [
		{
			key: "curl",
			title: t("models.detail.snippet.curl"),
			lang: "bash",
			copyText: snippetCurlModelText(modelSnippetOpts),
		},
		{
			key: "powershell",
			title: t("models.detail.snippet.powershell"),
			lang: "powershell",
			copyText: snippetPowershellModelText(modelSnippetOpts),
		},
		{
			key: "javascript",
			title: t("models.detail.snippet.javascript"),
			lang: "javascript",
			copyText: snippetJSModelText(modelSnippetOpts),
		},
		{
			key: "python",
			title: t("models.detail.snippet.python"),
			lang: "python",
			copyText: snippetPythonModelText(modelSnippetOpts),
		},
		{
			key: "claude",
			title: t("models.detail.snippet.claudeCode"),
			lang: "bash",
			copyText: snippetClaudeCodeModelText(modelSnippetOpts),
		},
		{
			key: "openclaw",
			title: t("models.detail.snippet.openClaw"),
			lang: "bash",
			copyText: snippetOpenClawModelText(modelSnippetOpts),
		},
		{
			key: "hermes",
			title: t("models.detail.snippet.hermes"),
			lang: "bash",
			copyText: snippetHermesModelText(modelSnippetOpts),
		},
		{
			key: "librechat",
			title: t("models.detail.snippet.librechat"),
			lang: "yaml",
			copyText: snippetLibreChatModelText(modelSnippetOpts),
		},
		{
			key: "zed",
			title: t("models.detail.snippet.zed"),
			lang: "json",
			copyText: snippetZedModelText(zedOpts),
		},
		{
			key: "opencode",
			title: t("models.detail.snippet.opencode"),
			lang: "json",
			copyText: snippetOpencodeModelText(opencodeOpts),
		},
	];
}
