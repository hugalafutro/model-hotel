import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { AutoSyncPanel } from "../AutoSyncPanel";

function member(id: string, name: string, hasToken = true): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: hasToken,
		created_at: "",
		updated_at: "",
		status: {
			health: { known: true, healthy: true, latency_ms: 1, checked_at: "" },
		},
	};
}

const members = [member("1", "hotel-1"), member("2", "hotel-2", false)];

function renderPanel() {
	render(
		<ToastProvider>
			<AutoSyncPanel members={members} />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	server.use(
		http.get("/api/fleet/autosync", () =>
			HttpResponse.json({ enabled: false, primary_id: "" }),
		),
	);
});

it("only offers token-holding members as primary", async () => {
	renderPanel();
	const select = await screen.findByRole("combobox");
	// hotel-1 has a token; hotel-2 does not, so it must not be selectable.
	expect(within(select).getByRole("option", { name: "hotel-1" })).toBeTruthy();
	expect(within(select).queryByRole("option", { name: "hotel-2" })).toBeNull();
});

it("requires a primary before enabling, then warns when active", async () => {
	let lastPut: { enabled: boolean; primary_id: string } | null = null;
	server.use(
		http.put("/api/fleet/autosync", async ({ request }) => {
			lastPut = (await request.json()) as typeof lastPut;
			return HttpResponse.json(lastPut);
		}),
	);
	renderPanel();

	// The enable toggle is disabled until a primary is chosen.
	const checkbox = await screen.findByRole("checkbox");
	expect(checkbox).toBeDisabled();

	await userEvent.selectOptions(screen.getByRole("combobox"), "1");
	await waitFor(() =>
		expect(lastPut).toEqual({ enabled: false, primary_id: "1" }),
	);
	await waitFor(() => expect(screen.getByRole("checkbox")).toBeEnabled());

	await userEvent.click(screen.getByRole("checkbox"));
	await waitFor(() =>
		expect(lastPut).toEqual({ enabled: true, primary_id: "1" }),
	);

	// The amber warning appears once auto-sync is active.
	expect(await screen.findByText(/Automatic sync is on/i)).toBeTruthy();
});

it("gates repointing an already-set primary behind the admin token", async () => {
	const puts: Array<{
		enabled: boolean;
		primary_id: string;
		confirm_token?: string;
	}> = [];
	server.use(
		http.get("/api/fleet/autosync", () =>
			HttpResponse.json({ enabled: true, primary_id: "1" }),
		),
		http.put("/api/fleet/autosync", async ({ request }) => {
			const body = (await request.json()) as (typeof puts)[number];
			puts.push(body);
			// Mirror the backend: refuse a primary change without the admin token.
			if (!body.confirm_token) {
				return new HttpResponse("confirm the admin token", { status: 403 });
			}
			return HttpResponse.json({ enabled: true, primary_id: body.primary_id });
		}),
	);
	render(
		<ToastProvider>
			<AutoSyncPanel
				members={[member("1", "hotel-1"), member("3", "hotel-3")]}
			/>
		</ToastProvider>,
	);

	// Pick a different primary: the change must be held, not persisted yet.
	await userEvent.selectOptions(await screen.findByRole("combobox"), "3");
	const dialog = await screen.findByRole("dialog");
	expect(puts).toHaveLength(0);

	// Confirm stays blocked until a token is entered.
	const confirmBtn = within(dialog).getByRole("button", {
		name: /confirm change/i,
	});
	expect(confirmBtn).toBeDisabled();

	await userEvent.type(
		within(dialog).getByLabelText(/admin token/i),
		"s3cret-token",
	);
	await userEvent.click(confirmBtn);

	await waitFor(() =>
		expect(puts).toContainEqual({
			enabled: true,
			primary_id: "3",
			confirm_token: "s3cret-token",
		}),
	);
});

it("recovers when a primary was set concurrently (stale snapshot)", async () => {
	// The client loaded with no primary, but another admin configured one in the
	// meantime, so the server gates the first un-tokened PUT with a 403.
	const puts: Array<{ primary_id: string; confirm_token?: string }> = [];
	server.use(
		http.put("/api/fleet/autosync", async ({ request }) => {
			const body = (await request.json()) as (typeof puts)[number];
			puts.push(body);
			if (!body.confirm_token) {
				return new HttpResponse("confirm the admin token", { status: 403 });
			}
			return HttpResponse.json({ enabled: false, primary_id: body.primary_id });
		}),
	);
	renderPanel();

	// Selecting a primary PUTs without a token and is refused; instead of an
	// error, the confirmation modal opens so the operator can recover.
	await userEvent.selectOptions(await screen.findByRole("combobox"), "1");
	const dialog = await screen.findByRole("dialog");
	await waitFor(() => expect(puts).toHaveLength(1));

	await userEvent.type(
		within(dialog).getByLabelText(/admin token/i),
		"s3cret-token",
	);
	await userEvent.click(
		within(dialog).getByRole("button", { name: /confirm change/i }),
	);

	await waitFor(() =>
		expect(puts).toContainEqual({
			enabled: false,
			primary_id: "1",
			confirm_token: "s3cret-token",
		}),
	);
});
