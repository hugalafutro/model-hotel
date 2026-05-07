import type React from "react";

export interface EmptyStateProps {
	message: string;
	action?: {
		label: string;
		onClick: () => void;
	};
	icon?: React.ComponentType<{ size?: number; className?: string }>;
	className?: string;
}

export function EmptyState({
	message,
	action,
	icon: Icon,
	className,
}: EmptyStateProps) {
	return (
		<div className={`text-center py-12 ${className ?? "ui-card"}`}>
			{Icon && <Icon size={32} className="mx-auto mb-3 text-gray-600" />}
			<p className="text-gray-500">{message}</p>
			{action && (
				<button
					type="button"
					onClick={action.onClick}
					className="ui-btn ui-btn-primary mt-4"
				>
					{action.label}
				</button>
			)}
		</div>
	);
}
