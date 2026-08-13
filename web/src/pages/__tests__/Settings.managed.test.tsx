import { screen } from "@testing-library/react";
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

// The managed banner claims everything below it is fleet-synced and read-only,
// so its placement is load-bearing: the instance-local sections
// (Authentication, Appearance, Observability, plus the partially-local Alerts)
// must sit above it and the six fully-synced sections below it.
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
			http.get("/api/settings", () => HttpResponse.json({})),
		);
		renderWithProviders(<Settings />);

		const banner = await screen.findByTestId("managed-banner");
		const notes = await screen.findAllByTestId("managed-note");

		// Six fully-synced sections render SettingsSection's note; Alerts renders
		// its own partial-managed note with the same testid. 6 + 1 = 7.
		expect(notes).toHaveLength(7);

		const above = notes.filter(
			(note) =>
				note.compareDocumentPosition(banner) & Node.DOCUMENT_POSITION_FOLLOWING,
		);
		// Exactly one note sits above the banner: the mixed Alerts section, which
		// stays in the local group because its Apprise delivery settings are
		// instance-local. Every fully-synced section sits below the banner, so
		// its "everything below is read-only" copy stays true.
		expect(above).toHaveLength(1);
	});

	it("renders no banner when this instance is the primary", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("primary")),
			),
			http.get("/api/settings", () => HttpResponse.json({})),
		);
		renderWithProviders(<Settings />);

		// Settle on a section being present, then assert the banner's absence.
		await screen.findAllByRole("heading", { level: 2 });
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
		expect(screen.queryByTestId("managed-note")).not.toBeInTheDocument();
	});
});
