import { describe, expect, it } from "vitest";
import type { ArenaPromptPreset, PersonaPreset } from "../presets";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../presets";

describe("CHAT_PERSONAS", () => {
	it("is a non-empty array", () => {
		expect(Array.isArray(CHAT_PERSONAS)).toBe(true);
		expect(CHAT_PERSONAS.length).toBeGreaterThan(0);
	});

	it("items have required fields: id, icon, label, systemPrompt", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		CHAT_PERSONAS.forEach((persona: PersonaPreset, _index: number) => {
			expect(persona).toHaveProperty("id");
			expect(persona).toHaveProperty("icon");
			expect(persona).toHaveProperty("label");
			expect(persona).toHaveProperty("systemPrompt");
		});
	});

	it("all ids are unique", () => {
		const ids = CHAT_PERSONAS.map((p: PersonaPreset) => p.id);
		const uniqueIds = new Set(ids);
		expect(uniqueIds.size).toBe(ids.length);
	});

	it("system prompts are i18n key paths", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		CHAT_PERSONAS.forEach((persona: PersonaPreset, _index: number) => {
			expect(typeof persona.systemPrompt).toBe("string");
			expect(persona.systemPrompt).toMatch(
				/^presets\.personas\.[a-z0-9-]+\.prompt$/,
			);
		});
	});
});

describe("ARENA_PROMPTS", () => {
	it("is a non-empty array", () => {
		expect(Array.isArray(ARENA_PROMPTS)).toBe(true);
		expect(ARENA_PROMPTS.length).toBeGreaterThan(0);
	});

	it("items have required fields: id, icon, label, prompt", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		ARENA_PROMPTS.forEach((prompt: ArenaPromptPreset, _index: number) => {
			expect(prompt).toHaveProperty("id");
			expect(prompt).toHaveProperty("icon");
			expect(prompt).toHaveProperty("label");
			expect(prompt).toHaveProperty("prompt");
		});
	});

	it("all ids are unique", () => {
		const ids = ARENA_PROMPTS.map((p: ArenaPromptPreset) => p.id);
		const uniqueIds = new Set(ids);
		expect(uniqueIds.size).toBe(ids.length);
	});

	it("prompts are i18n key paths", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		ARENA_PROMPTS.forEach((prompt: ArenaPromptPreset, _index: number) => {
			expect(typeof prompt.prompt).toBe("string");
			expect(prompt.prompt).toMatch(/^presets\.prompts\.[a-z0-9-]+\.text$/);
		});
	});
});
