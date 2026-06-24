import { type ReactNode, useEffect } from "react";

interface ModalProps {
	title: string;
	onClose: () => void;
	children: ReactNode;
	actions?: ReactNode;
}

// Lightweight modal: backdrop click and Escape close it. The control plane has
// only a handful of dialogs, so this stays deliberately minimal (no portal,
// no focus-trap library); it is rendered at the app root.
export function Modal({ title, onClose, children, actions }: ModalProps) {
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: backdrop click-to-dismiss is a convenience; Escape and the explicit close button are the keyboard-accessible paths.
		<div
			className="fd-modal-backdrop"
			onMouseDown={(e) => {
				if (e.target === e.currentTarget) onClose();
			}}
		>
			<div
				className="fd-modal"
				role="dialog"
				aria-modal="true"
				aria-label={title}
			>
				<h2>{title}</h2>
				<div>{children}</div>
				{actions && <div className="fd-modal-actions">{actions}</div>}
			</div>
		</div>
	);
}
