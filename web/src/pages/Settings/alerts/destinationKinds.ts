import type { DestinationKind } from "@web-shared/alerts/composers";
import {
	DiscordLogo,
	Link,
	type LucideIcon,
	Mail,
	Smartphone,
	TelegramLogo,
} from "@/lib/icons";

// Model Hotel has no companion app of its own, so the phone tile is ntfy's.
// (composers.ts still recognises a Bellhop topic when it describes a stored
// destination that Front Desk set up.)
export const KINDS = [
	"ntfy",
	"telegram",
	"discord",
	"email",
	"other",
] as const satisfies readonly DestinationKind[];
export const KIND_HINT: Record<(typeof KINDS)[number], string> = {
	ntfy: "kindNtfyHint",
	telegram: "kindTelegramHint",
	discord: "kindDiscordHint",
	email: "kindEmailHint",
	other: "kindOtherHint",
};
// Three tiles are named after the service itself, so they reuse the shared
// settings.alerts.kind.* labels. Two need more than the bare name to be picked
// correctly: "ntfy" means nothing until it says it is the phone app, and
// "Apprise URL" is the catch-all rather than a service.
export const KIND_TITLE: Partial<Record<(typeof KINDS)[number], string>> = {
	ntfy: "kindNtfyTitle",
	other: "kindOtherTitle",
};
// One glyph per tile, so the five options can be told apart at a glance
// before the titles are read: the two services with a logo wear it.
export const KIND_ICON: Record<(typeof KINDS)[number], LucideIcon> = {
	ntfy: Smartphone,
	telegram: TelegramLogo,
	discord: DiscordLogo,
	email: Mail,
	other: Link,
};
