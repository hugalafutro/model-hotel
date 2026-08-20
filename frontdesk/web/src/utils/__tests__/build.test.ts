import { describe, expect, it } from "vitest";
import {
	buildLabel,
	buildsDiffer,
	buildTitle,
	isDevVersion,
	stampedCommit,
} from "../build";

describe("isDevVersion", () => {
	it("treats only an exact semver tag as a release", () => {
		expect(isDevVersion("v1.2.3")).toBe(false);
		expect(isDevVersion("1.2.3")).toBe(false);
		expect(isDevVersion("dev")).toBe(true);
		// A `git describe` fallback is a dev build, not the tag it derives from.
		expect(isDevVersion("v1.2.3-15-gabc123")).toBe(true);
		expect(isDevVersion("v1.2.3-dirty")).toBe(true);
	});
});

describe("stampedCommit", () => {
	it("rejects the sentinels that name no build", () => {
		expect(stampedCommit("b80c04d4494f")).toBe(true);
		expect(stampedCommit("")).toBe(false);
		expect(stampedCommit("unknown")).toBe(false);
	});
});

describe("buildLabel", () => {
	it("shows the commit for a dev build, since every dev image reports 'dev'", () => {
		expect(buildLabel({ version: "dev", commit: "b80c04d4494f" })).toBe(
			"b80c04d4494f",
		);
	});

	it("keeps a release tag, which identifies itself", () => {
		expect(buildLabel({ version: "v1.2.3", commit: "b80c04d4494f" })).toBe(
			"v1.2.3",
		);
	});

	it("falls back to the version when no commit names the build", () => {
		expect(buildLabel({ version: "dev", commit: "" })).toBe("dev");
		expect(buildLabel({ version: "dev", commit: "unknown" })).toBe("dev");
	});
});

describe("buildTitle", () => {
	it("carries the half the label does not show", () => {
		expect(buildTitle({ version: "dev", commit: "b80c04d4494f" })).toBe(
			"dev · b80c04d4494f",
		);
	});

	it("is absent when there is no second half", () => {
		expect(buildTitle({ version: "dev", commit: "unknown" })).toBeUndefined();
		expect(buildTitle({ version: "", commit: "b80c04d4494f" })).toBeUndefined();
	});
});

describe("buildsDiffer", () => {
	// Mirrors internal/frontdesk/versionskew.go's buildSkew, which is what
	// actually holds config sync.
	it("differs on the version", () => {
		expect(
			buildsDiffer(
				{ version: "v1.0.0", commit: "" },
				{ version: "v0.9.0", commit: "" },
			),
		).toBe(true);
	});

	it("differs on the commit when the versions match", () => {
		expect(
			buildsDiffer(
				{ version: "dev", commit: "aaaaaaaaaaaa" },
				{ version: "dev", commit: "bbbbbbbbbbbb" },
			),
		).toBe(true);
	});

	it("agrees when both halves match", () => {
		expect(
			buildsDiffer(
				{ version: "dev", commit: "aaaaaaaaaaaa" },
				{ version: "dev", commit: "aaaaaaaaaaaa" },
			),
		).toBe(false);
	});

	it("fails closed when either version is unreadable", () => {
		// Mirrors the gate: an unread version on EITHER side is skew, so a primary
		// whose own build is unconfirmed holds the whole fleet rather than
		// silently passing it.
		expect(
			buildsDiffer({ version: "", commit: "" }, { version: "dev", commit: "" }),
		).toBe(true);
		expect(
			buildsDiffer({ version: "dev", commit: "" }, { version: "", commit: "" }),
		).toBe(true);
		expect(
			buildsDiffer({ version: "", commit: "" }, { version: "", commit: "" }),
		).toBe(true);
	});

	it("falls back to the version when either commit names no build", () => {
		expect(
			buildsDiffer(
				{ version: "dev", commit: "aaaaaaaaaaaa" },
				{ version: "dev", commit: "" },
			),
		).toBe(false);
		expect(
			buildsDiffer(
				{ version: "dev", commit: "aaaaaaaaaaaa" },
				{ version: "dev", commit: "unknown" },
			),
		).toBe(false);
	});
});
