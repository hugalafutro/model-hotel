import { useTranslation } from "react-i18next";

export interface LoadingSpinnerProps {
	className?: string;
	/**
	 * Renders the bare spinner at footer size, without the centred full-height
	 * box a page-level fallback needs.
	 */
	inline?: boolean;
}

export function LoadingSpinner({ className, inline }: LoadingSpinnerProps) {
	const { t } = useTranslation();
	const spinner = (
		<div
			data-testid="spinner"
			role="status"
			aria-label={t("common.loading")}
			className={`animate-spin rounded-full border-b-2 border-(--accent) ${inline ? "h-4 w-4" : "h-12 w-12"} ${className ?? ""}`}
		/>
	);
	if (inline) return spinner;
	return <div className="flex items-center justify-center h-64">{spinner}</div>;
}
