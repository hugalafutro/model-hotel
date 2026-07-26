import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "../ErrorBoundary";

function Boom(): never {
	throw new Error("payload is not what this build expected");
}

describe("ErrorBoundary", () => {
	// React logs every error it hands to a boundary, which is correct behaviour
	// but noisy here: these tests throw on purpose.
	beforeEach(() => {
		vi.spyOn(console, "error").mockImplementation(() => {});
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("renders its children while nothing throws", () => {
		render(
			<ErrorBoundary fallback={<p data-testid="fallback">nope</p>}>
				<p data-testid="child">fine</p>
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("child")).toBeInTheDocument();
		expect(screen.queryByTestId("fallback")).not.toBeInTheDocument();
	});

	it("renders the fallback and leaves the surrounding tree intact when a child throws", () => {
		render(
			<div>
				<p data-testid="sibling">still here</p>
				<ErrorBoundary fallback={<p data-testid="fallback">contained</p>}>
					<Boom />
				</ErrorBoundary>
			</div>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		// The point of the boundary: a throw inside it does not unmount the page.
		expect(screen.getByTestId("sibling")).toBeInTheDocument();
	});

	it("renders nothing at all when no fallback is given", () => {
		// How App wraps QuotaStrip: the strip already renders nothing when it has
		// nothing to show, so disappearing is its established empty state.
		render(
			<div data-testid="host">
				<ErrorBoundary>
					<Boom />
				</ErrorBoundary>
			</div>,
		);
		expect(screen.getByTestId("host")).toBeEmptyDOMElement();
	});
});
