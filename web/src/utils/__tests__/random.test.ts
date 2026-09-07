import { afterEach, describe, expect, it, vi } from "vitest";
import type { Model } from "../../api/types";
import { pickRandom, randomChatModelId } from "../random";

const model = (provider: string, id: string) =>
	({ provider_name: provider, model_id: id }) as Model;

describe("pickRandom", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("is undefined for an empty list", () => {
		expect(pickRandom([])).toBeUndefined();
	});

	it("picks by the random draw", () => {
		vi.spyOn(Math, "random").mockReturnValue(0.99);
		expect(pickRandom(["a", "b", "c"])).toBe("c");
	});
});

describe("randomChatModelId", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("never returns an excluded id", () => {
		vi.spyOn(Math, "random").mockReturnValue(0);
		const models = [model("OpenAI", "gpt-4o"), model("Anthropic", "claude")];
		expect(randomChatModelId(models, ["OpenAI/gpt-4o"])).toBe(
			"Anthropic/claude",
		);
	});

	it("is undefined when everything is excluded", () => {
		const models = [model("OpenAI", "gpt-4o")];
		expect(randomChatModelId(models, ["OpenAI/gpt-4o"])).toBeUndefined();
	});
});
