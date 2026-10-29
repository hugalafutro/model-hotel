import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import type { BracketRound } from "../types";
import { useArenaRunner } from "../useArenaRunner";
import { streamKey } from "../utils";
import { createMockDeps, createWrapper } from "./useArenaRunner.test.helpers";

// The runner's no-op guards (a retry aimed at a slot that does not exist, a
// dispatch after unmount, a swap into a missing matchup) and the running-set
// updaters retry and swap hand to setRunningModels.

const slot = (modelId: string) => ({
	modelId,
	personaId: null,
	personaPrompt: "",
	params: {},
});

function oneMatchup(slotB: ReturnType<typeof slot> | null): BracketRound[] {
	return [
		{
			matchups: [
				{
					slotA: slot("P/model-a"),
					slotB,
					responseA: null,
					responseB: null,
					vote: null,
				},
			],
		},
	];
}

describe("useArenaRunner guards", () => {
	it.each([
		["a round that does not exist", 5, 0, "A"],
		["a matchup that does not exist", 0, 3, "A"],
		["an empty slot", 0, 0, "B"],
	] as const)(
		"retrying %s does nothing",
		(_label, roundIdx, matchupIdx, key) => {
			const rounds = oneMatchup(null);
			const deps = createMockDeps({ rounds, roundsRef: { current: rounds } });
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});
			act(() => result.current.handleRetry(roundIdx, matchupIdx, key));
			expect(deps.setRounds).not.toHaveBeenCalled();
			expect(deps.setRunningModels).not.toHaveBeenCalled();
			expect(deps.setPhase).not.toHaveBeenCalled();
		},
	);

	it("a retry adds its slot to the running set", () => {
		const rounds = oneMatchup(slot("P/model-b"));
		const setRunningModels = vi.fn();
		const deps = createMockDeps({
			rounds,
			roundsRef: { current: rounds },
			setRunningModels,
		});
		server.use(
			http.post(
				"/api/chat/arena",
				() => new HttpResponse(null, { status: 500 }),
			),
		);
		const { result } = renderHook(() => useArenaRunner(deps), {
			wrapper: createWrapper(),
		});
		act(() => result.current.handleRetry(0, 0, "B"));
		const update = setRunningModels.mock.calls[0][0] as (
			prev: Set<string>,
		) => Set<string>;
		const prev = new Set(["other"]);
		expect(update(prev)).toEqual(new Set(["other", streamKey(0, 0, "B")]));
		// The previous set is not mutated in place.
		expect(prev).toEqual(new Set(["other"]));
		result.current.abortAll();
	});

	it("a swap into a missing matchup changes nothing and starts nothing", () => {
		const rounds = oneMatchup(null);
		const roundsRef = { current: rounds };
		const setRunningModels = vi.fn();
		const setPhase = vi.fn();
		const deps = createMockDeps({
			rounds,
			roundsRef,
			setRunningModels,
			setPhase,
		});
		const { result } = renderHook(() => useArenaRunner(deps), {
			wrapper: createWrapper(),
		});
		act(() => result.current.handleSwapComplete(0, 7, "A", "P/new-model"));
		// The bracket has no matchup 7: no slot is marked running, the phase
		// stays put, no stream is registered, and the rounds are untouched.
		expect(setRunningModels).not.toHaveBeenCalled();
		expect(setPhase).not.toHaveBeenCalled();
		expect(deps.setRounds).not.toHaveBeenCalled();
		expect(result.current.abortMapRef.current.size).toBe(0);
		expect(roundsRef.current).toEqual(rounds);
	});

	it("does not dispatch a stream once the arena has unmounted", () => {
		const deps = createMockDeps();
		const { result, unmount } = renderHook(() => useArenaRunner(deps), {
			wrapper: createWrapper(),
		});
		const { streamModel, abortMapRef } = result.current;
		unmount();
		// P/model-a is a valid chat model, so without the unmount guard the call
		// registers its abort controller synchronously before starting the
		// stream. Checking right after the call needs no wait.
		streamModel("P/model-a", "", "hi", 0, "A", 0);
		expect(abortMapRef.current.size).toBe(0);
		expect(deps.setRounds).not.toHaveBeenCalled();
	});
});
