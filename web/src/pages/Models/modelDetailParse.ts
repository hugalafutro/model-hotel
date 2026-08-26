import type { Model } from "../../api/types";

/** The model's stored params JSON, or null when it is missing or malformed. */
export function parseParams(raw: string): Record<string, unknown> | null {
	try {
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

/** A modalities column (JSON array or bare value) as a list; malformed = none. */
export function parseModalities(raw: string): string[] {
	try {
		const v = JSON.parse(raw);
		return Array.isArray(v) ? v : [v];
	} catch {
		return [];
	}
}

export function modelModalities(model: Model): {
	inputMods: string[];
	outputMods: string[];
} {
	return {
		inputMods: parseModalities(model.input_modalities),
		outputMods: parseModalities(model.output_modalities),
	};
}
