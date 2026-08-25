import { describeTarget } from "@web-shared/alerts/composers";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { CopyButton } from "../../../components/CopyButton";

// DestinationList renders the saved Apprise targets as one readable row each:
// which service it points at, which host, and the identifying segment (topic,
// bot token, mailbox password). The values are shown in clear, because anyone
// who can open this page can already read and rewrite them, and a masked list
// makes it impossible to tell two phones apart. Every row can be copied, tested
// on its own, and removed; the parent owns the list and persists the result.
export function DestinationList({
	targets,
	onRemove,
	onTest,
	busy,
	disabledReason,
	emptyText,
}: {
	/** Plaintext Apprise URLs, in stored order. */
	targets: string[];
	/** Called with the URL to drop; the parent persists the remaining list. */
	onRemove: (url: string) => void;
	/** Called with the URL to deliver a test notification to. */
	onTest: (url: string) => void;
	/** True while the card is saving or testing: row actions are blocked. */
	busy: boolean;
	// Why testing and removing are unavailable right now (e.g. the manual field
	// holds an unsaved change, so this list no longer describes what is stored).
	// Set: the note is shown, and both row actions are blocked and carry it as a
	// tooltip. Undefined: the rows act normally.
	disabledReason?: string;
	// What to say when there is nothing to list. Unset: the card's own "no
	// destinations yet" line, which is right when this list is the stored set.
	// Set: the caller's wording, for a list that is only part of the picture
	// (the wizard shows what one run added, not what is saved).
	emptyText?: string;
}) {
	const { t } = useTranslation();
	const [removing, setRemoving] = useState<string | null>(null);
	const blocked = busy || !!disabledReason;

	if (targets.length === 0) {
		return (
			<p
				className="text-xs text-(--text-muted)"
				data-testid="alert-destinations-empty"
			>
				{emptyText ?? t("settings.alerts.destinations.empty")}
			</p>
		);
	}

	return (
		<div className="space-y-1.5">
			{disabledReason && (
				<p
					className="text-xs text-(--text-muted)"
					data-testid="alert-destinations-dirty"
				>
					{disabledReason}
				</p>
			)}
			{targets.map((url) => {
				const info = describeTarget(url);
				const kindLabel = t(`settings.alerts.kind.${info.kind}`);
				// Every row carries the same three buttons, so the accessible name
				// says which destination it acts on.
				const rowName = `${kindLabel} ${info.host}`;
				return (
					<div
						key={url}
						data-testid="alert-destination-row"
						className="ui-detail-tile flex items-center gap-3 px-3 py-2 text-sm flex-wrap"
					>
						<span className="ui-badge ui-badge-neutral">{kindLabel}</span>
						<span className="text-(--text-secondary)">{info.host}</span>
						{/* The theme's own mono face: Tailwind's font-mono is a fixed
						    stack and would ignore the Terminal style's JetBrains Mono. */}
						<code
							className="text-xs text-(--text-primary) select-all break-all"
							style={{ fontFamily: "var(--font-mono)" }}
						>
							{info.secret || info.url}
						</code>
						<span className="flex items-center gap-1.5 ml-auto">
							{/* Puts one target URL on the clipboard so it can be pasted
							    into another Model Hotel or a service's own UI. */}
							<CopyButton
								variant="label"
								text={url}
								testId="alert-destination-copy"
								ariaLabel={`${t("common.copy")}: ${rowName}`}
							/>
							<button
								type="button"
								className="ui-btn ui-btn-secondary"
								data-testid="alert-destination-test"
								aria-label={`${t("settings.alerts.testRow")}: ${rowName}`}
								title={disabledReason}
								disabled={blocked}
								onClick={() => onTest(url)}
							>
								{t("settings.alerts.testRow")}
							</button>
							<button
								type="button"
								className="ui-btn ui-btn-secondary"
								data-testid="alert-destination-remove"
								aria-label={`${t("settings.alerts.removeRow")}: ${rowName}`}
								title={disabledReason}
								disabled={blocked}
								onClick={() => setRemoving(url)}
							>
								{t("settings.alerts.removeRow")}
							</button>
						</span>
					</div>
				);
			})}

			{removing !== null && (
				<ConfirmDialog
					title={t("settings.alerts.removeTitle")}
					message={t("settings.alerts.removeBody")}
					fields={[removing]}
					confirmLabel={t("settings.alerts.removeConfirm")}
					confirmTestId="alert-destination-remove-confirm"
					onConfirm={() => {
						onRemove(removing);
						setRemoving(null);
					}}
					onCancel={() => setRemoving(null)}
				/>
			)}
		</div>
	);
}
