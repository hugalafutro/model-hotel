export interface LoadingSpinnerProps {
	className?: string;
}

export function LoadingSpinner({ className }: LoadingSpinnerProps) {
	return (
		<div className="flex items-center justify-center h-64">
			<div
				data-testid="spinner"
				className={`animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent) ${className ?? ""}`}
			/>
		</div>
	);
}
