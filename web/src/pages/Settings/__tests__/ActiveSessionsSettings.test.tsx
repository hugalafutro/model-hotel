import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import i18next from "../../../i18n";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ActiveSessionsPanel } from "../ActiveSessionsSettings";

type SessionRow = {
	id: string;
	user_agent: string;
	ip: string;
	created_at: string;
	last_seen_at?: string;
	current: boolean;
};

const firefoxLinux =
	"Mozilla/5.0 (X11; Linux x86_64; rv:141.0) Gecko/20100101 Firefox/141.0";
const chromeAndroid =
	"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Mobile Safari/537.36";

const hereRow: SessionRow = {
	id: "11111111-1111-4111-8111-111111111111",
	user_agent: firefoxLinux,
	ip: "203.0.113.7",
	created_at: "2026-08-10T10:00:00Z",
	last_seen_at: "2026-08-13T08:00:00Z",
	current: true,
};
const phoneRow: SessionRow = {
	id: "22222222-2222-4222-8222-222222222222",
	user_agent: chromeAndroid,
	ip: "198.51.100.9",
	created_at: "2026-08-11T09:00:00Z",
	last_seen_at: "2026-08-12T21:00:00Z",
	current: false,
};

function mockSessions(...pages: SessionRow[][]) {
	let call = 0;
	server.use(
		http.get("/api/auth/sessions", () => {
			const rows = pages[Math.min(call, pages.length - 1)];
			call += 1;
			return HttpResponse.json({ sessions: rows });
		}),
	);
}

function mockRevoke(revoked: number) {
	const calls: number[] = [];
	server.use(
		http.post("/api/auth/sessions/revoke-others", () => {
			calls.push(1);
			return HttpResponse.json({ revoked });
		}),
	);
	return calls;
}

function mockRevokeOne() {
	const ids: string[] = [];
	server.use(
		http.delete("/api/auth/sessions/:id", ({ params }) => {
			ids.push(String(params.id));
			return new HttpResponse(null, { status: 204 });
		}),
	);
	return ids;
}

describe("ActiveSessionsPanel", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		mockSessions([hereRow, phoneRow]);
	});

	it("lists the sessions with device, IP, and current marker", async () => {
		renderWithProviders(<ActiveSessionsPanel />);

		const rows = await screen.findAllByTestId("auth-session-row");
		expect(rows).toHaveLength(2);
		// Device summaries parsed from the user agent, not the raw string.
		expect(screen.getByText(/Firefox/)).toBeInTheDocument();
		expect(screen.getByText(/Chrome/)).toBeInTheDocument();
		expect(screen.getByText(/203\.0\.113\.7/)).toBeInTheDocument();
		expect(screen.getByTestId("current-session-chip")).toBeInTheDocument();
	});

	// The current session is what logout is for; a sign-out button on it would
	// be a logout with worse labeling.
	it("offers per-session sign-out only on other sessions", async () => {
		renderWithProviders(<ActiveSessionsPanel />);

		await screen.findAllByTestId("auth-session-row");
		expect(screen.getAllByTestId("revoke-session")).toHaveLength(1);
	});

	it("signs out a single session after arm and confirm, then refreshes", async () => {
		mockSessions([hereRow, phoneRow], [hereRow]);
		const ids = mockRevokeOne();
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		await screen.findAllByTestId("auth-session-row");
		const button = screen.getByTestId("revoke-session");
		await user.click(button);
		expect(ids).toHaveLength(0);

		await user.click(button);
		await waitFor(() => expect(ids).toEqual([phoneRow.id]));
		await waitFor(() =>
			expect(screen.getAllByTestId("auth-session-row")).toHaveLength(1),
		);
	});

	// Sessions minted before the metadata migration have no user agent; they
	// must read as an unknown device, not an empty cell.
	it("labels a session with no user agent as an unknown device", async () => {
		mockSessions([
			hereRow,
			{ ...phoneRow, user_agent: "", ip: "", last_seen_at: undefined },
		]);
		renderWithProviders(<ActiveSessionsPanel />);

		await screen.findAllByTestId("auth-session-row");
		expect(
			screen.getByText(i18next.t("settings.activeSessions.unknownDevice")),
		).toBeInTheDocument();
	});

	// The action ends every other session, so a stray click must not fire it.
	it("arms on the first click and only revokes on the second", async () => {
		const calls = mockRevoke(2);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		expect(calls).toHaveLength(0);

		await user.click(button);
		await waitFor(() => expect(calls).toHaveLength(1));
	});

	it("reports how many sessions were signed out", async () => {
		mockRevoke(3);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		await user.click(button);

		// Resolved through i18next rather than hardcoded English, so the test
		// does not depend on the active locale. (A bare /3/ would also match the
		// listed IPs.)
		expect(
			await screen.findByText(
				i18next.t("settings.activeSessions.signedOut", { count: 3 }),
			),
		).toBeInTheDocument();
	});

	// Zero is its own message: "signed out 0 sessions" would read as a failure.
	it("says so when there was nothing else open", async () => {
		mockRevoke(0);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		await user.click(button);

		// Resolved through i18next rather than hardcoded English, so the test
		// does not depend on the active locale.
		expect(
			await screen.findByText(
				i18next.t("settings.activeSessions.noneToSignOut"),
			),
		).toBeInTheDocument();
	});

	it("surfaces a failure instead of implying the sessions are gone", async () => {
		server.use(
			http.post("/api/auth/sessions/revoke-others", () =>
				HttpResponse.json({ error: "nope" }, { status: 500 }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		await user.click(button);

		expect(await screen.findByText(/500/)).toBeInTheDocument();
	});
});
