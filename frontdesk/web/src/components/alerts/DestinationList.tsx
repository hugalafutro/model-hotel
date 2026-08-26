import { describeTarget } from "@web-shared/alerts/composers";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCopyToClipboard } from "../../hooks/useCopyToClipboard";
import { ConfirmModal } from "../ConfirmModal";

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
			<div
				className="fd-faint"
				data-testid="alert-destinations-empty"
				style={{ fontSize: "0.85rem" }}
			>
				{emptyText ?? t("settings.alerts.destinationsEmpty")}
			</div>
		);
	}

	return (
		<div className="fd-stack" style={{ gap: "0.4rem" }}>
			{disabledReason && (
				<div
					className="fd-faint"
					data-testid="alert-destinations-dirty"
					style={{ fontSize: "0.78rem" }}
				>
					{disabledReason}
				</div>
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
						className="fd-row"
						style={{
							gap: "0.5rem",
							flexWrap: "wrap",
							alignItems: "center",
						}}
					>
						<span className="ui-badge ui-badge-info">{kindLabel}</span>
						<span style={{ fontSize: "0.85rem" }}>{info.host}</span>
						<code
							className="fd-mono"
							style={{ fontSize: "0.8rem", userSelect: "all" }}
						>
							{info.secret || info.url}
						</code>
						<span
							className="fd-row"
							style={{ gap: "0.3rem", marginLeft: "auto" }}
						>
							<CopyButton url={url} rowName={rowName} />
							<button
								type="button"
								className="ui-btn ui-btn-sm"
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
								className="ui-btn ui-btn-sm"
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
				<ConfirmModal
					title={t("settings.alerts.removeTitle")}
					confirmLabel={t("settings.alerts.removeConfirm")}
					confirmTestId="alert-destination-remove-confirm"
					onConfirm={() => {
						onRemove(removing);
						setRemoving(null);
					}}
					onClose={() => setRemoving(null)}
				>
					<p>{t("settings.alerts.removeBody")}</p>
					<code className="fd-mono" style={{ fontSize: "0.8rem" }}>
						{removing}
					</code>
				</ConfirmModal>
			)}
		</div>
	);
}

// CopyButton puts one target URL on the clipboard so it can be pasted into
// another Front Desk or a service's own UI. A blocked clipboard is silent; the
// row text stays selectable.
function CopyButton({ url, rowName }: { url: string; rowName: string }) {
	const { t } = useTranslation();
	// The "Copied" label reverts on a timer the hook drops if the row goes first:
	// removing a destination unmounts it, and firing then would set state on an
	// unmounted button.
	const { copy, copied } = useCopyToClipboard();
	return (
		<button
			type="button"
			className="ui-btn ui-btn-sm"
			data-testid="alert-destination-copy"
			aria-label={`${t("common.copy")}: ${rowName}`}
			onClick={() => {
				void copy(url);
			}}
		>
			{copied ? t("common.copied") : t("common.copy")}
		</button>
	);
}
