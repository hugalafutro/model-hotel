import { X } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef } from "react";

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
	const ref = useRef<HTMLDivElement>(null);

	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		},
		[onClose],
	);

	useEffect(() => {
		ref.current?.focus();
	}, []);

	return (
		<div
			ref={ref}
			role="dialog"
			aria-modal="true"
			tabIndex={-1}
			className={`fixed inset-0 flex items-center justify-center ${zIndex} outline-none`}
			onKeyDown={handleKeyDown}
		>
			<button
				type="button"
				className="absolute inset-0 bg-black/60 cursor-default"
				onClick={closeOnBackdrop ? onClose : undefined}
				aria-label="Close dialog"
			/>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: stopPropagation prevents backdrop click bubbling */}
			{/* biome-ignore lint/a11y/useKeyWithClickEvents: purely structural click propagation control */}
			<div
				className={`relative ui-card p-6 w-full ${maxWidth}${
					scrollable ? " max-h-[85vh] overflow-y-auto" : ""
				}`}
				onClick={(e) => e.stopPropagation()}
			>
				<button
					type="button"
					onClick={onClose}
					className="absolute top-3 right-3 z-10 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-pointer p-2 hover:drop-shadow-[var(--glow-accent-lg)]"
					aria-label="Close"
				>
					<X size={20} />
				</button>
				{header
					? header
					: title && (
							<h2 className="text-xl font-bold text-white mb-4">{title}</h2>
						)}
				{children}
			</div>
		</div>
	);
}
