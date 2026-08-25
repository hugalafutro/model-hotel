import type { Range } from "./types";

/**
 * X-axis label for one timeseries bucket, at the resolution its range implies:
 * a day ("Jun 15") over a week, minutes ("14:35") over an hour, the hour alone
 * ("14:00") over a day. The date part follows the browser locale; the clock
 * parts are zero-padded 24-hour so labels stay the same width on a narrow axis.
 */
export function bucketLabel(date: Date, range: Range): string {
	if (range === "1w") {
		return date.toLocaleDateString(undefined, {
			month: "short",
			day: "numeric",
		});
	}
	const hours = date.getHours().toString().padStart(2, "0");
	if (range === "1h") {
		return `${hours}:${date.getMinutes().toString().padStart(2, "0")}`;
	}
	return `${hours}:00`;
}
