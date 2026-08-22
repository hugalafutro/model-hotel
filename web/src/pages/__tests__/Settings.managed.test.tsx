import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockSystemStats } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Settings } from "../Settings";

// Full Settings page mount; same per-file budget as Settings.test.tsx (see the
// note there for why this is per-file rather than a global bump).
const SETTINGS_PAGE_TIMEOUT_MS = 45_000;

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

const precedes = (a: Element, b: Element) =>
	Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);

// The managed banner claims the configuration below it is fleet-synced and
// read-only, so its placement is load-bearing: the instance-local sections
// (Authentication, Appearance, Observability, Alerts - the first and last
// only partially, each behind its own note) must sit above it and the six
// fully-synced sections below it.
describe("Settings managed (fleet member) mode", {
	timeout: SETTINGS_PAGE_TIMEOUT_MS,
}, () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("renders the banner between the local sections and the synced ones", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
			// oidc_enabled exposes the issuer input, the probe for the disabled
			// state of Authentication's synced half.
			http.get("/api/settings", () =>
				HttpResponse.json({ oidc_enabled: "true" }),
			),
		);
		renderWithProviders(<Settings />);

		const banner = await screen.findByTestId("managed-banner");

		// Two-sided boundary, pinned by section headings so a local section
		// drifting below the banner fails as loudly as a synced one drifting
		// above it: 4 local sections precede the banner, 6 synced follow.
		const headings = screen.getAllByRole("heading", { level: 2 });
		expect(headings).toHaveLength(10);
		expect(headings.filter((h) => precedes(h, banner))).toHaveLength(4);

		// Six fully-synced sections render SettingsSection's note; the mixed
		// Authentication and Alerts sections render their own partial notes
		// with the same testid. 6 + 2 = 8, and only the two partial notes sit
		// above the banner.
		const notes = await screen.findAllByTestId("managed-note");
		expect(notes).toHaveLength(8);
		expect(notes.filter((n) => precedes(n, banner))).toHaveLength(2);

		// The SSO provider config is per-member, so its inputs stay live on a
		// managed member; only the fleet-synced email allowlist is disabled,
		// and the instance-local tab-timeout slider stays live too.
		expect(await screen.findByTestId("oidc-issuer-input")).not.toBeDisabled();
		expect(
			await screen.findByTestId("oidc-allowed-emails-input"),
		).toBeDisabled();
		expect(
			await screen.findByTestId("github-allowed-emails-input"),
		).toBeDisabled();
		expect(document.getElementById("session-idle-timeout")).not.toBeDisabled();
	});

	it("renders the primary banner on the same boundary, and no notes, when this instance is the primary", async () => {
		let systemServed = false;
		server.use(
			http.get("/api/system", () => {
				systemServed = true;
				return HttpResponse.json(systemWithFleet("primary"));
			}),
			http.get("/api/settings", () =>
				HttpResponse.json({ oidc_enabled: "true" }),
			),
		);
		renderWithProviders(<Settings />);

		// Settle on the fleet-state query actually resolving, not just the
		// sections painting: useManaged reports false while loading, so
		// asserting absence before the response lands would pass vacuously.
		await screen.findAllByRole("heading", { level: 2 });
		await waitFor(() => expect(systemServed).toBe(true));

		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
		expect(screen.queryByTestId("managed-note")).not.toBeInTheDocument();

		// The primary's counterpart banner sits on the same boundary: the
		// four per-member sections above it, the six fleet-synced ones below.
		const banner = await screen.findByTestId("primary-banner");
		const headings = screen.getAllByRole("heading", { level: 2 });
		expect(headings).toHaveLength(10);
		expect(headings.filter((h) => precedes(h, banner))).toHaveLength(4);

		expect(await screen.findByTestId("oidc-issuer-input")).not.toBeDisabled();
		expect(
			await screen.findByTestId("oidc-allowed-emails-input"),
		).not.toBeDisabled();
		expect(
			await screen.findByTestId("github-allowed-emails-input"),
		).not.toBeDisabled();
	});
});
