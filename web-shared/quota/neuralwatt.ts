// NeuralWatt credit-balance parsing shared by both SPAs.

import type { NeuralWattBalanceLike } from "./types";

/**
 * Dollars drawn from the NeuralWatt credit balance that the provider has not
 * yet settled into the total.
 *
 * The provider's `credits_used_usd` cannot be trusted: verified live on
 * 2026-08-24, it stays 0 while overage spend visibly drains
 * `credits_remaining_usd` (NeuralWatt's own dashboard renders the same
 * misleading $0.00). Also verified: `total_credits_usd` re-bases down to
 * `remaining` as spend settles (within about a minute), so this difference is
 * a floor on the draw, not a cumulative figure, and is usually 0 outside an
 * active burn. Callers therefore must not render it as a "spent" dollar
 * amount; it feeds bar math, where 0 degrades to the pre-derivation look.
 * Assumes total >= remaining at account load; the reported field still wins
 * when it is the larger number, so an upstream fix can never make this read
 * lower than what NeuralWatt admits to.
 */
export function getNeuralWattCreditsSpent(b: NeuralWattBalanceLike): number {
	const reported = b.credits_used_usd ?? 0;
	const total = b.total_credits_usd ?? 0;
	const remaining = b.credits_remaining_usd;
	if (remaining == null || total <= 0) return reported;
	return Math.max(reported, Math.max(0, total - remaining));
}
