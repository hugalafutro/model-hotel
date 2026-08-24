// NeuralWatt credit-balance parsing shared by both SPAs.

import type { NeuralWattBalanceLike } from "./types";

/**
 * Dollars actually drawn from the NeuralWatt credit balance.
 *
 * The provider's `credits_used_usd` cannot be trusted: verified live on
 * 2026-08-24, it stays 0 while overage spend visibly drains
 * `credits_remaining_usd` (NeuralWatt's own dashboard renders the same
 * misleading $0.00). The real draw is therefore derived as total − remaining.
 * The reported field still wins when it is the larger number, so an upstream
 * fix can never make this read lower than what NeuralWatt admits to.
 */
export function getNeuralWattCreditsSpent(b: NeuralWattBalanceLike): number {
	const reported = b.credits_used_usd ?? 0;
	const total = b.total_credits_usd ?? 0;
	const remaining = b.credits_remaining_usd;
	if (remaining == null || total <= 0) return reported;
	return Math.max(reported, Math.max(0, total - remaining));
}
