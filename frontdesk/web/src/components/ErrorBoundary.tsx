import { Component, type ReactNode } from "react";

interface ErrorBoundaryProps {
	children: ReactNode;
	/** Rendered in place of the children once they have thrown. */
	fallback?: ReactNode;
}

interface ErrorBoundaryState {
	failed: boolean;
}

/**
 * Containment for one subtree.
 *
 * React unmounts the WHOLE tree when a render throws with no boundary above it,
 * so a single widget that chokes on a malformed payload takes the entire control
 * plane down with it. Wrapping the widget confines the blast radius to that
 * widget. Deliberately minimal: no retry, no reporting, no error UI of its own,
 * because the only thing the operator needs is for the rest of the page to keep
 * working.
 *
 * This has to be a class: getDerivedStateFromError has no hook equivalent.
 */
export class ErrorBoundary extends Component<
	ErrorBoundaryProps,
	ErrorBoundaryState
> {
	state: ErrorBoundaryState = { failed: false };

	static getDerivedStateFromError(): ErrorBoundaryState {
		return { failed: true };
	}

	render() {
		if (this.state.failed) return this.props.fallback ?? null;
		return this.props.children;
	}
}
