import { describe, expect, it } from "vitest";
import {
	snippetCurl,
	snippetLibreChatText,
	snippetOpenClawText,
	snippetOpencode,
	snippetZed,
} from "../snippets";

describe("snippets", () => {
	describe("snippetCurl", () => {
		it("returns curl command with correct proxyModelId and origin", () => {
			const result = snippetCurl({
				proxyModelId: "provider/model",
				origin: "https://example.com",
			});
			expect(result).toContain("provider/model");
			expect(result).toContain("https://example.com/v1/chat/completions");
		});

		it("includes /v1/chat/completions path and Bearer auth", () => {
			const result = snippetCurl({
				proxyModelId: "test-model",
				origin: "https://api.example.com",
			});
			expect(result).toContain("-X POST");
			expect(result).toContain("Authorization: Bearer API_KEY");
			expect(result).toContain("Content-Type: application/json");
			expect(result).toContain("/v1/chat/completions");
		});
	});

	describe("snippetZed", () => {
		it("returns valid JSON with correct structure", () => {
			const result = snippetZed({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: { tool_calling: true, vision: false, reasoning: true },
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.language_models.openai_compatible["model-hotel"]
					.available_models[0].name,
			).toBe("p/m");
			expect(
				parsed.language_models.openai_compatible["model-hotel"].api_url,
			).toBe("https://example.com/v1");
		});

		it("sets tools/images/parallel_tool_calls based on capabilities", () => {
			const result = snippetZed({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: {
					tool_calling: true,
					vision: true,
					parallel_tool_calls: true,
					reasoning: false,
				},
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			const caps =
				parsed.language_models.openai_compatible["model-hotel"]
					.available_models[0].capabilities;
			expect(caps.tools).toBe(true);
			expect(caps.images).toBe(true);
			expect(caps.parallel_tool_calls).toBe(true);
			expect(caps.interleaved_reasoning).toBe(false);
		});

		it("handles null capabilities (all false)", () => {
			const result = snippetZed({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			const caps =
				parsed.language_models.openai_compatible["model-hotel"]
					.available_models[0].capabilities;
			expect(caps.tools).toBe(false);
			expect(caps.images).toBe(false);
			expect(caps.parallel_tool_calls).toBe(false);
			expect(caps.interleaved_reasoning).toBe(false);
		});
	});

	describe("snippetOpencode", () => {
		it("returns valid JSON with correct provider structure", () => {
			const result = snippetOpencode({
				proxyModelId: "provider/model",
				displayName: "Test Model",
				contextLength: 16384,
				maxOutputTokens: 8192,
				capabilities: { reasoning: false, tool_calling: false },
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(parsed.provider["model-hotel"].name).toBe("Model Hotel");
			expect(parsed.provider["model-hotel"].npm).toBe(
				"@ai-sdk/openai-compatible",
			);
			expect(parsed.provider["model-hotel"].options.baseURL).toBe(
				"https://example.com/v1",
			);
		});

		it("sets attachment=true when inputModalities includes non-text", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Vision Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["image", "text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models["Vision Model"].attachment,
			).toBe(true);
		});

		it("sets attachment=false when inputModalities is only text", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Text Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models["Text Model"].attachment,
			).toBe(false);
		});

		it("sets reasoning=true when capabilities has reasoning=true", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Reasoning Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: { reasoning: true, tool_calling: false },
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models["Reasoning Model"].reasoning,
			).toBe(true);
		});

		it("sets tool_call=true when capabilities has tool_calling=true", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Tool Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: { reasoning: false, tool_calling: true },
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models["Tool Model"].tool_call,
			).toBe(true);
		});

		it("uses [text] as default when inputModalities is empty", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: [],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models.Model.modalities.input,
			).toEqual(["text"]);
		});

		it("uses [text] as default when outputModalities is empty", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: [],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models.Model.modalities.output,
			).toEqual(["text"]);
		});

		it("includes cost object when both prices are non-null", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: 0.5,
				outputPricePerMillion: 1.5,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(parsed.provider["model-hotel"].models.Model.cost).toEqual({
				input: 0.5,
				output: 1.5,
			});
		});

		it("omits cost object when inputPricePerMillion is null", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: 1.5,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(parsed.provider["model-hotel"].models.Model.cost).toBeUndefined();
		});

		it("omits cost object when outputPricePerMillion is null", () => {
			const result = snippetOpencode({
				proxyModelId: "p/m",
				displayName: "Model",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: 0.5,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(parsed.provider["model-hotel"].models.Model.cost).toBeUndefined();
		});

		it("uses correct displayName as model key and proxyModelId as id", () => {
			const result = snippetOpencode({
				proxyModelId: "provider/actual-model-id",
				displayName: "My Custom Model Name",
				contextLength: 8192,
				maxOutputTokens: 4096,
				capabilities: null,
				inputModalities: ["text"],
				outputModalities: ["text"],
				inputPricePerMillion: null,
				outputPricePerMillion: null,
				origin: "https://example.com",
			});
			const parsed = JSON.parse(result);
			expect(
				parsed.provider["model-hotel"].models["My Custom Model Name"],
			).toBeDefined();
			expect(
				parsed.provider["model-hotel"].models["My Custom Model Name"].id,
			).toBe("provider/actual-model-id");
		});
	});

	describe("snippetOpenClawText", () => {
		it("uses model-hotel provider name instead of wafer", () => {
			const result = snippetOpenClawText({ origin: "https://example.com" });
			expect(result).toContain("models.providers.model-hotel");
			expect(result).toContain("model-hotel/model_name");
			expect(result).not.toContain("wafer");
		});

		it("includes correct origin URL", () => {
			const result = snippetOpenClawText({ origin: "https://example.com" });
			expect(result).toContain("https://example.com/v1");
		});
	});

	describe("snippetLibreChatText", () => {
		it("includes Model Hotel as name and modelDisplayLabel", () => {
			const result = snippetLibreChatText({
				origin: "https://example.com",
			});
			expect(result).toContain('name: "Model Hotel"');
			expect(result).toContain('modelDisplayLabel: "Model Hotel"');
		});

		it("includes correct origin baseURL", () => {
			const result = snippetLibreChatText({
				origin: "https://example.com",
			});
			expect(result).toContain('baseURL: "https://example.com/v1"');
		});

		it("includes model_name as default model", () => {
			const result = snippetLibreChatText({
				origin: "https://example.com",
			});
			expect(result).toContain('- "model_name"');
		});

		it("includes API_KEY variable reference", () => {
			const result = snippetLibreChatText({
				origin: "https://example.com",
			});
			// biome-ignore lint/suspicious/noTemplateCurlyInString: intentional YAML variable check
			expect(result).toContain("${API_KEY}");
		});
	});
});
