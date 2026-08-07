import type { ZaiCodingLimitLike, ZaiCodingResponseLike } from "./types";

// Z.ai reports one entry per window in `data.limits`, keyed by `type` plus a
// `unit` code: 3 is the rolling 5-hour window, 6 the weekly one. The entry is
// returned as the caller declared it, so an app that models more of the payload
// keeps its own fields.

export function getZaiCodingFiveHourLimit<L extends ZaiCodingLimitLike>(
	data: ZaiCodingResponseLike<L> | undefined | null,
): L | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
}

export function getZaiCodingWeeklyLimit<L extends ZaiCodingLimitLike>(
	data: ZaiCodingResponseLike<L> | undefined | null,
): L | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
}
