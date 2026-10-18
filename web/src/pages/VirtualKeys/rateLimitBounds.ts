/** The bounds each rate-limit input accepts, matching the API's. */
export const RATE_LIMIT_BOUNDS = {
	rps: { min: "0", max: "10000", step: "any" },
	burst: { min: "1", max: "10000" },
	tpm: { min: "1", max: "100000000" },
} as const;
