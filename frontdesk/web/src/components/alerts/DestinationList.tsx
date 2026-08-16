import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmModal } from "../ConfirmModal";
import { describeTarget } from "./composers";

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
}: {
	/** Plaintext Apprise URLs, in stored order. */
	targets: string[];
	/** Called with the URL to drop; the parent persists the remaining list. */
	onRemove: (url: string) => void;
	/** Called with the URL to deliver a test notification to. */
	onTest: (url: string) => void;
	/** True while the card is saving or testing: row actions are blocked. */
	busy: boolean;
}) {
	const { t } = useTranslation();
	const [removing, setRemoving] = useState<string | null>(null);

	if (targets.length === 0) {
		return (
			<div
				className="fd-faint"
				data-testid="alert-destinations-empty"
				style={{ fontSize: "0.85rem" }}
			>
				{t("settings.alerts.destinationsEmpty")}
			</div>
		);
	}

	return (
		<div className="fd-stack" style={{ gap: "0.4rem" }}>
			{targets.map((url) => {
				const info = describeTarget(url);
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
						<span className="ui-badge ui-badge-info">
							{t(`settings.alerts.kind.${info.kind}`)}
						</span>
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
							<CopyButton url={url} />
							<button
								type="button"
								className="ui-btn ui-btn-sm"
								data-testid="alert-destination-test"
								disabled={busy}
								onClick={() => onTest(url)}
							>
								{t("settings.alerts.testRow")}
							</button>
							<button
								type="button"
								className="ui-btn ui-btn-sm"
								data-testid="alert-destination-remove"
								disabled={busy}
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
function CopyButton({ url }: { url: string }) {
	const { t } = useTranslation();
	const [copied, setCopied] = useState(false);
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(url);
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		} catch {
			/* clipboard blocked: the value stays selectable */
		}
	};
	return (
		<button
			type="button"
			className="ui-btn ui-btn-sm"
			data-testid="alert-destination-copy"
			onClick={copy}
		>
			{copied ? t("common.copied") : t("common.copy")}
		</button>
	);
}
