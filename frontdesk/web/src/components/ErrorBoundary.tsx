import { Component, type ReactNode } from "react";

interface ErrorBoundaryProps {
	children: ReactNode;
	/** Rendered in place of the children once they have thrown. */
	fallback?: ReactNode;
	/**
	 * Recovery trigger. A boundary that has already failed clears its failed
	 * state, and gives the children another go, whenever the CONTENTS of this
	 * array change (compared element-wise with Object.is, so a fresh array
	 * literal on every render is fine and expected).
	 *
	 * Without it a boundary latches for the lifetime of the mount, which for a
	 * boundary inside a shell that never unmounts means "until the operator
	 * reloads the page". Pass something that changes when the situation the
	 * children choked on plausibly has: the tab they are rendered next to, the
	 * identity of the payload they render. Omit it to keep the latching
	 * behaviour, which is the right call when the parent already discards the
	 * boundary itself (an unmount, or a changing `key`).
	 */
	resetKeys?: unknown[];
}

interface ErrorBoundaryState {
	failed: boolean;
}

/** True when the two key lists differ in length or in any element. */
function keysChanged(a: unknown[] | undefined, b: unknown[] | undefined) {
	if (a === b) return false;
	if (!a || !b) return true;
	if (a.length !== b.length) return true;
	return a.some((v, i) => !Object.is(v, b[i]));
}

/**
 * Containment for one subtree.
 *
 * React unmounts the WHOLE tree when a render throws with no boundary above it,
 * so a single widget that chokes on a malformed payload takes the entire control
 * plane down with it. Wrapping the widget confines the blast radius to that
 * widget. Deliberately minimal: no reporting and no error UI of its own, because
 * the only thing the operator needs is for the rest of the page to keep working.
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

	// Recovery is driven by a CHANGE in resetKeys, never by "we re-rendered", so
	// children that throw again immediately cannot spin: the retry render sets
	// `failed` back to true, and by the time this runs again prevProps carries
	// the same keys as this.props, so nothing resets until the operator's
	// situation actually changes.
	componentDidUpdate(prevProps: ErrorBoundaryProps) {
		if (!this.state.failed) return;
		if (keysChanged(prevProps.resetKeys, this.props.resetKeys)) {
			this.setState({ failed: false });
		}
	}

	render() {
		if (this.state.failed) return this.props.fallback ?? null;
		return this.props.children;
	}
}
