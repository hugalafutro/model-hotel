import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal } from "./Modal";

// ConfirmModal is the shared cancel/confirm dialog for Front Desk's destructive
// actions. Pass `ackLabel` to gate the confirm button behind an acknowledgement
// checkbox (used by the admin-token sync/reset flows); omit it for a plain
// confirm (e.g. removing a member). The ack state lives here and resets whenever
// the caller unmounts the modal, so callers only track whether it is open.
export function ConfirmModal({
	title,
	confirmLabel,
	confirmDisabled,
	busy,
	busyLabel,
	ackLabel,
	onConfirm,
	onClose,
	children,
}: {
	title: string;
	confirmLabel: string;
	// True while the confirmed action is in flight, to block a double-submit.
	confirmDisabled?: boolean;
	// True while the action runs: shows a spinner + busyLabel on the confirm
	// button and disables Cancel, so a slow action (e.g. config sync, which runs
	// model discovery on every member) reads as working rather than frozen.
	busy?: boolean;
	// Label shown beside the spinner while busy; falls back to confirmLabel.
	busyLabel?: string;
	// When set, an acknowledgement checkbox must be ticked before confirming.
	ackLabel?: string;
	onConfirm: () => void;
	onClose: () => void;
	children: ReactNode;
}) {
	const { t } = useTranslation();
	const [ack, setAck] = useState(false);
	const blocked = (!!ackLabel && !ack) || !!confirmDisabled || !!busy;

	return (
		<Modal
			title={title}
			onClose={onClose}
			actions={
				<>
					<button
						type="button"
						className="ui-btn"
						disabled={!!busy}
						onClick={onClose}
					>
						{t("common.cancel")}
					</button>
					<button
						type="button"
						className="ui-btn ui-btn-danger"
						disabled={blocked}
						aria-busy={busy}
						onClick={onConfirm}
					>
						{busy && <span className="fd-spinner" aria-hidden="true" />}
						{busy ? (busyLabel ?? confirmLabel) : confirmLabel}
					</button>
				</>
			}
		>
			{children}
			{ackLabel && (
				<label className="fd-row" style={{ cursor: "pointer" }}>
					<input
						type="checkbox"
						checked={ack}
						onChange={(e) => setAck(e.target.checked)}
					/>
					<span>{ackLabel}</span>
				</label>
			)}
		</Modal>
	);
}
