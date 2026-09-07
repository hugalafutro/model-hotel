import { describe, expect, it } from "vitest";
import { ApiError } from "../../../../api/client";
import {
	ALERT_TEST_PREFIX,
	appriseTone,
	SETTINGS_UPDATE_PREFIX,
	safeApiMessage,
	stripApiHead,
	TONE_LABEL,
} from "../apiText";

// The card and the wizard render server errors through these, so a drift in
// either the prefixes or the 400-only policy is a leak or a lost sentence.
const t = ((key: string, opts?: { defaultValue?: string }) =>
	opts?.defaultValue ?? key) as never;

describe("stripApiHead", () => {
	it("removes the fetchOK head and the status number", () => {
		expect(
			stripApiHead(
				`${SETTINGS_UPDATE_PREFIX}: 400 apprise url must be http(s)`,
				SETTINGS_UPDATE_PREFIX,
			),
		).toBe("apprise url must be http(s)");
	});

	it("leaves a message without the head alone", () => {
		expect(stripApiHead("network down", SETTINGS_UPDATE_PREFIX)).toBe(
			"network down",
		);
	});
});

describe("appriseTone", () => {
	it("maps reachable and healthy to success", () => {
		expect(
			appriseTone({ configured: true, reachable: true, healthy: true }),
		).toBe("success");
		expect(TONE_LABEL.success).toBe("reachable");
	});

	it("maps reachable but unhealthy to warning", () => {
		expect(
			appriseTone({ configured: true, reachable: true, healthy: false }),
		).toBe("warning");
		expect(TONE_LABEL.warning).toBe("issues");
	});

	it("maps unreachable, and an absent probe, to error", () => {
		expect(
			appriseTone({ configured: true, reachable: false, healthy: false }),
		).toBe("error");
		expect(appriseTone(undefined)).toBe("error");
		expect(TONE_LABEL.error).toBe("unreachable");
	});
});

describe("safeApiMessage", () => {
	it("shows the server sentence from a 400", () => {
		const err = new ApiError(
			`${ALERT_TEST_PREFIX}: 400 target is not a valid apprise url`,
			400,
		);
		expect(safeApiMessage(err, ALERT_TEST_PREFIX, t)).toBe(
			"target is not a valid apprise url",
		);
	});

	it("hides anything that is not a 400", () => {
		const err = new ApiError(`${ALERT_TEST_PREFIX}: 500 goroutine dump`, 500);
		expect(safeApiMessage(err, ALERT_TEST_PREFIX, t)).toBe(
			"common.unknownError",
		);
	});

	it("hides a plain Error", () => {
		expect(safeApiMessage(new Error("boom"), ALERT_TEST_PREFIX, t)).toBe(
			"common.unknownError",
		);
	});
});
