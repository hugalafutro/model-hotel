import { useTranslation } from "react-i18next";
import type { CapNote } from "../api/types";
import { formatTime, formatTimestamp } from "../utils/format";

// What the gateway cannot know, made explicit. A provider with no usage API
// (a plain OpenAI-compatible endpoint, or Ollama Cloud, whose account API says
// the plan but never the usage) has no quota badge to show; the one reading
// the gateway ever gets from it is the 429 whose body says the window or
// balance is spent. This badge shows the last of those, so an operator who
// saw a "90% used" email knows that was the last real reading and that the
// cap has been hit since, without guessing which window.
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
			className="px-2 py-1.5 text-xs font-medium ui-badge ui-badge-warning cursor-help"
			data-testid="cap-note-badge"
			title={tip}
		>
			{note.entitled
				? t("components.capNote.labelEntitled", { time })
				: t("components.capNote.label", { time })}
		</span>
	);
}
