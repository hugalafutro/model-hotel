import { X } from "lucide-react";
import { type ReactNode, useCallback } from "react";

interface ModalProps {
	title?: string;
	header?: ReactNode;
	closeOnBackdrop?: boolean;
	onClose: () => void;
	maxWidth?: string;
	scrollable?: boolean;
	children: ReactNode;
	zIndex?: string;
}

export function Modal({
	title,
	header,
	closeOnBackdrop = true,
	onClose,
	maxWidth = "max-w-md",
	scrollable = false,
	children,
	zIndex = "z-50",
}: ModalProps) {
	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		},
		[onClose],
	);

	return (
		<div
			role="dialog"
			aria-modal="true"
			className={`fixed inset-0 flex items-center justify-center ${zIndex}`}
			onKeyDown={handleKeyDown}
		>
			<button
				type="button"
				className="absolute inset-0 bg-black/60 cursor-default"
				onClick={closeOnBackdrop ? onClose : undefined}
				aria-label="Close dialog"
			/>
			<div
				className={`relative ui-card p-6 w-full ${maxWidth}${
					scrollable ? " max-h-[85vh] overflow-y-auto" : ""
				}`}
			>
				{header ? (
					header
				) : title ? (
					<>
						<button
							type="button"
							onClick={onClose}
							className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
							aria-label="Close"
						>
							<X size={20} />
						</button>
						<h2 className="text-xl font-bold text-white mb-4">{title}</h2>
					</>
				) : null}
				{children}
			</div>
		</div>
	);
}
