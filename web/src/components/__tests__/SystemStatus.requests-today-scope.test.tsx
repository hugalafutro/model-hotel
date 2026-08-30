import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
import { renderWithProviders } from "../../test/utils";
import { SystemStatus } from "../SystemStatus";

// GET /api/system reports requests_today scoped to the caller's own virtual
// keys for a non-admin and across every key for an admin, so the sidebar label
// has to say which of the two the number is. Every assertion reads the copy
// back through i18next by key rather than as an English literal, so the suite
// stays locale-independent.

const STAT_ALL = "layout.stats.requestsTodayAll";
const STAT_OWN = "layout.stats.requestsTodayOwn";
const TIP_ALL = "layout.tooltips.requestsTodayAll";
const TIP_OWN = "layout.tooltips.requestsTodayOwn";

// The caller's role is the input under test, so it is driven directly instead
// of through the /api/auth/me round trip: the provider reports admin while that
// request is still in flight, and a label asserted during that window would
// pass whichever role the component actually read.
const identity = vi.hoisted(() => ({ isAdmin: true }));

vi.mock("../../context/IdentityContext", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../../context/IdentityContext")>();
	return {
		...actual,
		useIdentity: () => ({
			me: null,
			isLoading: false,
			isAdmin: identity.isAdmin,
			can: () => identity.isAdmin,
		}),
	};
});

describe("SystemStatus requests-today scope", () => {
	it("gives the two scopes distinct copy", () => {
		for (const key of [STAT_ALL, STAT_OWN, TIP_ALL, TIP_OWN]) {
			expect(i18n.t(key)).not.toBe(key);
		}
		expect(i18n.t(STAT_ALL)).not.toBe(i18n.t(STAT_OWN));
		expect(i18n.t(TIP_ALL)).not.toBe(i18n.t(TIP_OWN));
	});

	it("labels the count as covering every key for an admin", () => {
		identity.isAdmin = true;
		renderWithProviders(<SystemStatus />);

		const label = screen.getByText(i18n.t(STAT_ALL));
		expect(label.closest("div")).toHaveAttribute("title", i18n.t(TIP_ALL));
		expect(screen.queryByText(i18n.t(STAT_OWN))).not.toBeInTheDocument();
	});

	it("labels the count as the caller's own for a non-admin", () => {
		identity.isAdmin = false;
		renderWithProviders(<SystemStatus />);

		const label = screen.getByText(i18n.t(STAT_OWN));
		expect(label.closest("div")).toHaveAttribute("title", i18n.t(TIP_OWN));
		expect(screen.queryByText(i18n.t(STAT_ALL))).not.toBeInTheDocument();
	});
});
