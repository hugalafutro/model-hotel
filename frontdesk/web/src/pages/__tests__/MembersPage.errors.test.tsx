import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import i18n from "../../i18n";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { MembersPage } from "../MembersPage";

// Failure paths of the members page: a rejected drain, a cancelled removal and
// each coded add-member refusal. UI text is looked up through its i18n key.

function member(id: string, name: string): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: false,
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		status: {
			health: {
				known: true,
				healthy: true,
				latency_ms: 12,
				checked_at: new Date().toISOString(),
			},
		},
	};
}

function renderPage() {
	return render(
		<ToastProvider>
			<MembersPage />
		</ToastProvider>,
	);
}

const rowOf = (name: string) =>
	screen.getByText(name).closest("tr") as HTMLElement;

beforeEach(() => {
	server.use(
		sseHandler(),
		http.get("/api/fleet/autosync", () =>
			HttpResponse.json({ enabled: false, primary_id: "" }),
		),
		http.get(
			"/api/fleet/last-sync",
			() => new HttpResponse(null, { status: 204 }),
		),
	);
});

describe("MembersPage failure paths", () => {
	it("toasts the generic error when a drain is rejected", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member("1", "hotel-1"), member("2", "hotel-2")]),
			),
			http.post("/api/members/1/state", () =>
				HttpResponse.json({ error: "nope" }, { status: 500 }),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		await userEvent.click(
			within(rowOf("hotel-1")).getByRole("button", {
				name: i18n.t("members.drain"),
			}),
		);
		await waitFor(() =>
			expect(
				within(screen.getByRole("status")).getByText(i18n.t("errors.generic")),
			).toBeInTheDocument(),
		);
		// Still active: the row keeps its Drain action.
		expect(
			within(rowOf("hotel-1")).getByRole("button", {
				name: i18n.t("members.drain"),
			}),
		).toBeInTheDocument();
	});

	it("cancelling the remove confirmation deletes nothing", async () => {
		let deleted = false;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member("1", "hotel-1"),
					member("2", "hotel-2"),
					member("3", "hotel-3"),
				]),
			),
			http.delete("/api/members/1", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");
		await userEvent.click(
			within(rowOf("hotel-1")).getByRole("button", {
				name: i18n.t("common.remove"),
			}),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: i18n.t("common.cancel") }),
		);
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		expect(deleted).toBe(false);
	});

	it.each([
		["duplicate", 409, "members.errDuplicate"],
		["unreachable", 400, "members.errUnreachable"],
		["identity_unverified", 400, "members.errIdentityUnverified"],
	])("maps the %s refusal to its own message", async (code, status, key) => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post("/api/members", () =>
				HttpResponse.json({ code, error: "server wording" }, { status }),
			),
		);
		renderPage();
		await screen.findByText(i18n.t("members.empty"));
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.nameLabel")),
			"h1",
		);
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.urlLabel")),
			"https://h1.example.com",
		);
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.tokenLabel")),
			"tok",
		);
		await userEvent.click(
			screen.getByRole("button", { name: i18n.t("common.add") }),
		);
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(i18n.t(key));
		expect(alert).not.toHaveTextContent("server wording");
	});

	it("shows the generic error for a failure that is not a validation refusal", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post("/api/members", () =>
				HttpResponse.json({ error: "internal detail" }, { status: 500 }),
			),
		);
		renderPage();
		await screen.findByText(i18n.t("members.empty"));
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.nameLabel")),
			"h1",
		);
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.urlLabel")),
			"https://h1.example.com",
		);
		await userEvent.type(
			screen.getByLabelText(i18n.t("members.tokenLabel")),
			"tok",
		);
		await userEvent.click(
			screen.getByRole("button", { name: i18n.t("common.add") }),
		);
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(i18n.t("errors.generic"));
		expect(alert).not.toHaveTextContent("internal detail");
	});
});
