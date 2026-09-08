/**
 * The red notice a form shows when its submit failed. `role="alert"` so the
 * message is announced: the operator's focus is on the button they just
 * pressed, not on the box that appeared above it.
 */
export function ErrorCallout({
	children,
	className = "",
	testId,
}: {
	children: React.ReactNode;
	className?: string;
	testId?: string;
}) {
	return (
		<div
			role="alert"
			data-testid={testId}
			className={`ui-callout ui-callout-error${className ? ` ${className}` : ""}`}
		>
			{children}
		</div>
	);
}
