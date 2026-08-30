import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Me } from "../../api/types";
import i18n from "../../i18n";
import { renderWithProviders } from "../../test/utils";
import { SystemStatus } from "../SystemStatus";

// GET /api/system reports requests_today scoped to the caller's own virtual
// keys for a non-admin and across every key for an admin, so the sidebar label
// has to say which of the two the number is. Every assertion reads the copy
// back through i18next by key rather than as an English literal, so the suite
// stays locale-independent.

const STAT_NEUTRAL = "layout.stats.requestsToday";
const STAT_ALL = "layout.stats.requestsTodayAll";
const STAT_OWN = "layout.stats.requestsTodayOwn";
const TIP_NEUTRAL = "layout.tooltips.requestsToday";
const TIP_ALL = "layout.tooltips.requestsTodayAll";
const TIP_OWN = "layout.tooltips.requestsTodayOwn";

// The caller's resolved identity is the input under test, so it is driven
// directly instead of through the /api/auth/me round trip: the provider reports
// admin both while that request is in flight and after it fails, and a label
// asserted in the first window would pass whichever role the component read.
// isAdmin below mirrors the real provider's fail-open rule so a component that
// reached for it instead of `me` would still see "admin" with no identity.
const identity = vi.hoisted(() => ({ me: null as Me | null }));

vi.mock("../../context/IdentityContext", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../../context/IdentityContext")>();
	return {
		...actual,
		useIdentity: () => ({
			me: identity.me,
			isLoading: false,
			isAdmin: !identity.me || identity.me.role === "admin",
			can: () => true,
		}),
	};
});

function asRole(role: Me["role"]): Me {
	return { username: "someone", role, grants: [] };
}

describe("SystemStatus requests-today scope", () => {
	it("gives each scope distinct copy", () => {
		const keys = [
			STAT_NEUTRAL,
			STAT_ALL,
			STAT_OWN,
			TIP_NEUTRAL,
			TIP_ALL,
			TIP_OWN,
		];
		for (const key of keys) {
			expect(i18n.t(key)).not.toBe(key);
		}
		expect(new Set(keys.map((k) => i18n.t(k))).size).toBe(keys.length);
	});

	it("labels the count as covering every key for an admin", () => {
		identity.me = asRole("admin");
		renderWithProviders(<SystemStatus />);

		const label = screen.getByText(i18n.t(STAT_ALL));
		expect(label.closest("div")).toHaveAttribute("title", i18n.t(TIP_ALL));
		expect(screen.queryByText(i18n.t(STAT_OWN))).not.toBeInTheDocument();
		expect(screen.queryByText(i18n.t(STAT_NEUTRAL))).not.toBeInTheDocument();
	});

	it("labels the count as the caller's own for a non-admin", () => {
		identity.me = asRole("user");
		renderWithProviders(<SystemStatus />);

		const label = screen.getByText(i18n.t(STAT_OWN));
		expect(label.closest("div")).toHaveAttribute("title", i18n.t(TIP_OWN));
		expect(screen.queryByText(i18n.t(STAT_ALL))).not.toBeInTheDocument();
		expect(screen.queryByText(i18n.t(STAT_NEUTRAL))).not.toBeInTheDocument();
	});

	// A failed /api/auth/me leaves the provider reporting admin forever, so a
	// label taken from isAdmin would caption a non-admin's own count as
	// everyone's with nothing to correct it. With no identity the label names no
	// scope rather than guessing one.
	it("claims no scope while the identity is unresolved or errored", () => {
		identity.me = null;
		renderWithProviders(<SystemStatus />);

		const label = screen.getByText(i18n.t(STAT_NEUTRAL));
		expect(label.closest("div")).toHaveAttribute("title", i18n.t(TIP_NEUTRAL));
		expect(screen.queryByText(i18n.t(STAT_ALL))).not.toBeInTheDocument();
		expect(screen.queryByText(i18n.t(STAT_OWN))).not.toBeInTheDocument();
	});
});
