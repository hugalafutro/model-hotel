import { useEffect, useRef } from "react";

/**
 * The red notice a form shows when its submit failed. `role="alert"` so the
 * message is announced: the operator's focus is on the button they just
 * pressed, not on the box that appeared above it. For the same reason it
 * scrolls itself into view when it appears or its message changes: in a
 * dialog whose body scrolls under a pinned Save button, the box can land
 * above the part of the form the operator is looking at.
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
	const ref = useRef<HTMLDivElement>(null);
	// biome-ignore lint/correctness/useExhaustiveDependencies: children is the trigger, a new message scrolls it back into view
	useEffect(() => {
		ref.current?.scrollIntoView({ block: "nearest" });
	}, [children]);
	return (
		<div
			ref={ref}
			role="alert"
			data-testid={testId}
			className={`ui-callout ui-callout-error${className ? ` ${className}` : ""}`}
		>
			{children}
		</div>
	);
}
