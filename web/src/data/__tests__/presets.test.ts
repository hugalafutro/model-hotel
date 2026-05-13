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

	it("system prompts are non-empty strings longer than 100 chars", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		CHAT_PERSONAS.forEach((persona: PersonaPreset, _index: number) => {
			expect(typeof persona.systemPrompt).toBe("string");
			expect(persona.systemPrompt.length).toBeGreaterThan(100);
		});
	});

	it("includes merlin persona", () => {
		const merlin = CHAT_PERSONAS.find((p: PersonaPreset) => p.id === "merlin");
		expect(merlin).toBeDefined();
		expect(merlin?.label).toBe("Merlin");
		expect(merlin?.icon).toBe("🧙");
	});

	it("includes madame-vex persona", () => {
		const vex = CHAT_PERSONAS.find((p: PersonaPreset) => p.id === "madame-vex");
		expect(vex).toBeDefined();
		expect(vex?.label).toBe("Madame Vex");
		expect(vex?.icon).toBe("🔮");
	});

	it("includes sarge persona", () => {
		const sarge = CHAT_PERSONAS.find((p: PersonaPreset) => p.id === "sarge");
		expect(sarge).toBeDefined();
		expect(sarge?.label).toBe("Sarge");
		expect(sarge?.icon).toBe("🦾");
	});

	it("includes grimm persona", () => {
		const grimm = CHAT_PERSONAS.find((p: PersonaPreset) => p.id === "grimm");
		expect(grimm).toBeDefined();
		expect(grimm?.label).toBe("Grimm");
		expect(grimm?.icon).toBe("💀");
	});

	it("includes nonna-pia persona", () => {
		const nonna = CHAT_PERSONAS.find(
			(p: PersonaPreset) => p.id === "nonna-pia",
		);
		expect(nonna).toBeDefined();
		expect(nonna?.label).toBe("Nonna Pia");
		expect(nonna?.icon).toBe("🍝");
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

	it("prompts are non-empty strings", () => {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		ARENA_PROMPTS.forEach((prompt: ArenaPromptPreset, _index: number) => {
			expect(typeof prompt.prompt).toBe("string");
			expect(prompt.prompt.length).toBeGreaterThan(0);
		});
	});

	it("includes dilemma prompt", () => {
		const dilemma = ARENA_PROMPTS.find(
			(p: ArenaPromptPreset) => p.id === "dilemma",
		);
		expect(dilemma).toBeDefined();
		expect(dilemma?.label).toBe("Dilemma");
		expect(dilemma?.icon).toBe("🧩");
		expect(dilemma?.prompt).toContain("locked room");
	});

	it("includes lore prompt", () => {
		const lore = ARENA_PROMPTS.find((p: ArenaPromptPreset) => p.id === "lore");
		expect(lore).toBeDefined();
		expect(lore?.label).toBe("Lore");
		expect(lore?.icon).toBe("📜");
	});

	it("includes trolley prompt", () => {
		const trolley = ARENA_PROMPTS.find(
			(p: ArenaPromptPreset) => p.id === "trolley",
		);
		expect(trolley).toBeDefined();
		expect(trolley?.label).toBe("Trolley Problem");
		expect(trolley?.icon).toBe("⚖️");
		expect(trolley?.prompt).toContain("runaway trolley");
	});

	it("includes algorithm prompt", () => {
		const algorithm = ARENA_PROMPTS.find(
			(p: ArenaPromptPreset) => p.id === "algorithm",
		);
		expect(algorithm).toBeDefined();
		expect(algorithm?.label).toBe("Algorithm");
		expect(algorithm?.icon).toBe("💻");
		expect(algorithm?.prompt).toContain("is_almost_prime");
	});

	it("includes eulogy prompt", () => {
		const eulogy = ARENA_PROMPTS.find(
			(p: ArenaPromptPreset) => p.id === "eulogy",
		);
		expect(eulogy).toBeDefined();
		expect(eulogy?.label).toBe("Eulogy");
		expect(eulogy?.icon).toBe("🕯️");
	});
});
