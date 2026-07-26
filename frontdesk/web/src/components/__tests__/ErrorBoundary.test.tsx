import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "../ErrorBoundary";

function Boom(): never {
	throw new Error("payload is not what this build expected");
}

function MaybeBoom({ boom }: { boom: boolean }) {
	if (boom) throw new Error("payload is not what this build expected");
	return <p data-testid="child">fine</p>;
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

	it("gives the children another go once resetKeys change", () => {
		const { rerender } = render(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();

		rerender(
			<ErrorBoundary
				resetKeys={["traffic"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom={false} />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("child")).toBeInTheDocument();
		expect(screen.queryByTestId("fallback")).not.toBeInTheDocument();
	});

	it("stays failed across re-renders that do not change resetKeys", () => {
		// The half that makes the reset meaningful: recovery is driven by a
		// CHANGE in the keys, not by "the parent re-rendered". A boundary that
		// cleared itself on any render would un-fail here even though nothing
		// about the operator's situation moved, and (with a child that keeps
		// throwing) would spin render-throw-reset forever.
		const { rerender } = render(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();

		// Same keys, by value: a fresh array literal each render is the normal
		// call shape, so identity must not be what counts.
		rerender(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom={false} />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		expect(screen.queryByTestId("child")).not.toBeInTheDocument();
	});

	it("settles back into the fallback when the retry throws again", () => {
		const { rerender } = render(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		// Changed keys, still-broken child: the retry throws, the boundary
		// re-fails, and it must stop there rather than looping (a loop shows up
		// here as the test never returning).
		rerender(
			<ErrorBoundary
				resetKeys={["traffic"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		expect(screen.queryByTestId("child")).not.toBeInTheDocument();
	});

	it("keeps latching when no resetKeys are given", () => {
		// How App wrapped the strip before recovery existed, and still how the
		// modal boundary inside QuotaStrip is wrapped: there the parent's
		// changing `key` throws the boundary away instead.
		const { rerender } = render(
			<ErrorBoundary fallback={<p data-testid="fallback">contained</p>}>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		rerender(
			<ErrorBoundary fallback={<p data-testid="fallback">contained</p>}>
				<MaybeBoom boom={false} />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		expect(screen.queryByTestId("child")).not.toBeInTheDocument();
	});

	it("resets when resetKeys appear on a boundary that had none", () => {
		const { rerender } = render(
			<ErrorBoundary fallback={<p data-testid="fallback">contained</p>}>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		rerender(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom={false} />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("child")).toBeInTheDocument();
	});

	it("resets when a key is added to the list", () => {
		// Length alone must count: two lists that agree on every shared element
		// are still different situations if one carries an extra key.
		const { rerender } = render(
			<ErrorBoundary
				resetKeys={["members"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("fallback")).toBeInTheDocument();
		rerender(
			<ErrorBoundary
				resetKeys={["members", "nanogpt:nano"]}
				fallback={<p data-testid="fallback">contained</p>}
			>
				<MaybeBoom boom={false} />
			</ErrorBoundary>,
		);
		expect(screen.getByTestId("child")).toBeInTheDocument();
	});
});
