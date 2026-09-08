/**
 * Tint applied to a model card when it stands for a chosen or highlighted
 * model. Shared so the reply card and the detail panel tint identically.
 */
export const CARD_TINT_CLASS = {
	accent: "ui-card-tint-accent",
	blue: "ui-card-tint-blue",
	default: "",
} as const;

export type CardTint = keyof typeof CARD_TINT_CLASS;
