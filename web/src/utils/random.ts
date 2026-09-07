import type { Model } from "../api/types";
import { proxyModelID } from "./model";

/** A uniformly random element, or undefined when there is nothing to pick from. */
export function pickRandom<T>(items: readonly T[]): T | undefined {
	if (items.length === 0) return undefined;
	return items[Math.floor(Math.random() * items.length)];
}

/**
 * A random proxy model id from `models`, skipping the ones in `exclude` (the
 * slot's current pick, and in the arena the models already on the board), so
 * pressing the dice always changes something. Undefined when every candidate
 * is excluded.
 */
export function randomChatModelId(
	models: readonly Model[],
	exclude: Iterable<string>,
): string | undefined {
	const taken = new Set(exclude);
	const available = models
		.map((m) => proxyModelID(m.provider_name, m.model_id))
		.filter((id) => !taken.has(id));
	return pickRandom(available);
}
