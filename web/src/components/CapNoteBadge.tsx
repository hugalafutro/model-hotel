import { useTranslation } from "react-i18next";
import type { CapNote } from "../api/types";
import { formatTime, formatTimestamp } from "../utils/format";

// Shows the last exhausted 429 a provider answered. A provider with no usage
// API (a plain OpenAI-compatible endpoint, or Ollama Cloud, whose account API
// names the plan but never the usage) has no quota badge, so that 429 is the
// only cap reading the gateway gets from it.
export function CapNoteBadge({ note }: { note: CapNote }) {
	const { t } = useTranslation();
	const valid = !Number.isNaN(new Date(note.at).getTime());
	const when = valid ? formatTimestamp(note.at) : note.at;
	const time = valid ? formatTime(note.at) : note.at;
	const tip = note.phrase
		? t("components.capNote.tip", {
				phrase: note.phrase,
				when,
				model: note.model,
			})
		: t("components.capNote.tipNoPhrase", { when, model: note.model });
	return (
		<span
			className="px-2 py-px leading-[1.6] text-xs font-medium ui-badge ui-badge-warning cursor-help"
			data-testid="cap-note-badge"
			title={tip}
		>
			{note.entitled
				? t("components.capNote.labelEntitled", { time })
				: t("components.capNote.label", { time })}
		</span>
	);
}
