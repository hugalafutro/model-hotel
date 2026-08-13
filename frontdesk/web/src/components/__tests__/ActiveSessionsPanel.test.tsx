import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import { ToastProvider } from "../../context/ToastContext";
import i18next from "../../i18n";
import { server } from "../../test/server";
import { ActiveSessionsPanel } from "../ActiveSessionsPanel";

type SessionRow = {
	id: string;
	user_agent: string;
	ip: string;
	created_at: string;
	last_seen_at?: string;
	current: boolean;
};

const hereRow: SessionRow = {
	id: "11111111-1111-4111-8111-111111111111",
	user_agent:
		"Mozilla/5.0 (X11; Linux x86_64; rv:141.0) Gecko/20100101 Firefox/141.0",
	ip: "203.0.113.7",
	created_at: "2026-08-10T10:00:00Z",
	last_seen_at: "2026-08-13T08:00:00Z",
	current: true,
};
const phoneRow: SessionRow = {
	id: "22222222-2222-4222-8222-222222222222",
	user_agent:
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Mobile Safari/537.36",
	ip: "198.51.100.9",
	created_at: "2026-08-11T09:00:00Z",
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

function renderPanel() {
	return render(
		<ToastProvider>
			<ActiveSessionsPanel />
		</ToastProvider>,
	);
}

beforeEach(() => {
	mockSessions([hereRow, phoneRow]);
});

it("lists sessions with device summaries and marks the current one", async () => {
	renderPanel();

	const rows = await screen.findAllByTestId("auth-session-row");
	expect(rows).toHaveLength(2);
	expect(screen.getByText(/Firefox/)).toBeInTheDocument();
	expect(screen.getByText(/203\.0\.113\.7/)).toBeInTheDocument();
	expect(screen.getByTestId("current-session-chip")).toBeInTheDocument();
	// Only the non-current row gets a sign-out button.
	expect(screen.getAllByTestId("revoke-session")).toHaveLength(1);
});

it("signs out one session through the confirm modal, then refreshes", async () => {
	mockSessions([hereRow, phoneRow], [hereRow]);
	const ids: string[] = [];
	server.use(
		http.delete("/api/auth/sessions/:id", ({ params }) => {
			ids.push(String(params.id));
			return new HttpResponse(null, { status: 204 });
		}),
	);
	const user = userEvent.setup();
	renderPanel();

	await user.click(await screen.findByTestId("revoke-session"));
	expect(ids).toHaveLength(0);

	await user.click(
		screen.getByRole("button", {
			name: i18next.t("settings.sessions.confirmSignOut"),
		}),
	);
	await waitFor(() => expect(ids).toEqual([phoneRow.id]));
	await waitFor(() =>
		expect(screen.getAllByTestId("auth-session-row")).toHaveLength(1),
	);
});

it("signs out all other sessions through the confirm modal", async () => {
	mockSessions([hereRow, phoneRow], [hereRow]);
	const calls: number[] = [];
	server.use(
		http.post("/api/auth/sessions/revoke-others", () => {
			calls.push(1);
			return HttpResponse.json({ revoked: 1 });
		}),
	);
	const user = userEvent.setup();
	renderPanel();

	await screen.findAllByTestId("auth-session-row");
	await user.click(screen.getByTestId("revoke-other-sessions"));
	expect(calls).toHaveLength(0);

	await user.click(
		screen.getByRole("button", {
			name: i18next.t("settings.sessions.confirmSignOutOthers"),
		}),
	);
	await waitFor(() => expect(calls).toHaveLength(1));
});

it("labels a session with no user agent as an unknown device", async () => {
	mockSessions([hereRow, { ...phoneRow, user_agent: "", ip: "" }]);
	renderPanel();

	await screen.findAllByTestId("auth-session-row");
	expect(
		screen.getByText(i18next.t("settings.sessions.unknownDevice")),
	).toBeInTheDocument();
});
