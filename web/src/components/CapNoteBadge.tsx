import { useTranslation } from "react-i18next";
import type { CapNote } from "../api/types";

// What the gateway cannot know, made explicit. A provider with no usage API
// (a plain OpenAI-compatible endpoint, or Ollama Cloud, whose account API says
// the plan but never the usage) has no quota badge to show; the one reading
// the gateway ever gets from it is the 429 whose body says the window or
// balance is spent. This badge shows the last of those, so an operator who
// saw a "90% used" email knows that was the last real reading and that the
// cap has been hit since, without guessing which window.
export function CapNoteBadge({ note }: { note: CapNote }) {
	const { t } = useTranslation();
	const at = new Date(note.at);
	const when = Number.isNaN(at.getTime()) ? note.at : at.toLocaleString();
	const time = Number.isNaN(at.getTime()) ? note.at : at.toLocaleTimeString();
	const tip = note.phrase
		? t("components.capNote.tip", {
				phrase: note.phrase,
				when,
				model: note.model,
				status: note.status,
			})
		: t("components.capNote.tipNoPhrase", {
				when,
				model: note.model,
				status: note.status,
			});
	return (
		<span
			className="px-2 py-1.5 text-xs font-medium ui-badge ui-badge-warning cursor-help"
			data-testid="cap-note-badge"
			title={tip}
		>
			{t("components.capNote.label", { time })}
		</span>
	);
}
